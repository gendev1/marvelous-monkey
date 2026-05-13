package overlay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
	"gopkg.in/yaml.v3"
)

// supportedSchemaVersion is the only overlay YAML schema version this
// loader accepts. Bumping the schema means writing a migrator (or
// versioned loader); silently accepting a future version would let
// shape changes slip past validation.
const supportedSchemaVersion = "1"

// rawRulebook mirrors the YAML schema for an overlay rule file. One
// instance per input file is decoded; LoadRulebook merges constants
// across files (rejecting key collisions) and concatenates rules in
// input order, then sorts deterministically.
type rawRulebook struct {
	SchemaVersion string         `yaml:"schema_version"`
	Source        string         `yaml:"source,omitempty"`
	Constants     map[string]any `yaml:"constants,omitempty"`
	Rules         []rawRule      `yaml:"rules,omitempty"`
}

type rawRule struct {
	ID                 string      `yaml:"id"`
	Priority           int         `yaml:"priority,omitempty"`
	Scope              string      `yaml:"scope"`
	GroupBy            string      `yaml:"group_by,omitempty"`
	Applies            AppliesSpec `yaml:"applies,omitempty"`
	When               string      `yaml:"when,omitempty"`
	Mode               string      `yaml:"mode"`
	Basis              string      `yaml:"basis,omitempty"`
	Formula            string      `yaml:"formula,omitempty"`
	Reason             string      `yaml:"reason,omitempty"`
	OnMissingReference string      `yaml:"on_missing_reference,omitempty"`
}

// LoadRulebook reads one or more overlay YAML files, validates them,
// pre-compiles every CEL `when` and `formula`, and returns a compiled
// Rulebook. Files are read in input order; constants from later files
// may not collide with earlier files (reject-on-collision per epic
// edge case). Rules are sorted by (priority asc, fileIndex asc,
// declIndex asc, id asc) so evaluation order is deterministic across
// runs and platforms.
//
// All validation errors are prefixed with "invalid overlay rulebook:"
// per the acceptance criteria. Errors are returned as plain Go errors
// (D6) — no CLI-flavored framing.
//
// Per D4/D5: rules may omit currency; the loader does not validate or
// filter on Applies.Currencies. Currency comes from Account.Currency at
// evaluation time.
func LoadRulebook(paths ...string) (*Rulebook, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("invalid overlay rulebook: no input files")
	}

	var (
		hashBuf      bytes.Buffer
		mergedConsts = map[string]any{}
		constSource  = map[string]string{} // key -> path that first set it
		allRules     []overlayRule
		seenID       = map[string]string{} // id -> path where first seen
	)

	for fileIndex, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("invalid overlay rulebook: read %s: %w", path, err)
		}
		// Include every input file's bytes in the audit hash, even
		// empty ones — the audit trail must honestly record what was
		// loaded (epic edge case).
		if fileIndex > 0 {
			hashBuf.WriteByte(0)
		}
		hashBuf.Write(data)

		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		var raw rawRulebook
		if err := dec.Decode(&raw); err != nil {
			// io.EOF on empty file is fine — treat as no contribution.
			if errors.Is(err, io.EOF) {
				continue
			}
			return nil, fmt.Errorf("invalid overlay rulebook: parse %s: %w", path, err)
		}
		if raw.SchemaVersion != supportedSchemaVersion {
			return nil, fmt.Errorf("invalid overlay rulebook: %s schema_version %q is unsupported (want %q)", path, raw.SchemaVersion, supportedSchemaVersion)
		}

		for k, v := range raw.Constants {
			if prev, dup := constSource[k]; dup {
				return nil, fmt.Errorf("invalid overlay rulebook: constant %q redefined in %s (first defined in %s)", k, path, prev)
			}
			mergedConsts[k] = normalizeConst(v)
			constSource[k] = path
		}

		for declIndex, rr := range raw.Rules {
			if prev, dup := seenID[rr.ID]; dup {
				return nil, fmt.Errorf("invalid overlay rulebook: duplicate rule id %q in %s (first seen in %s)", rr.ID, path, prev)
			}
			if rr.ID != "" {
				seenID[rr.ID] = path
			}
			if rr.OnMissingReference == "" {
				rr.OnMissingReference = "warn"
			}
			if err := validateRawRule(path, declIndex, rr); err != nil {
				return nil, err
			}
			allRules = append(allRules, overlayRule{
				ID:                 rr.ID,
				Priority:           rr.Priority,
				Scope:              rr.Scope,
				GroupBy:            rr.GroupBy,
				Applies:            rr.Applies,
				When:               rr.When,
				Mode:               rr.Mode,
				Basis:              rr.Basis,
				Formula:            rr.Formula,
				Reason:             rr.Reason,
				OnMissingReference: rr.OnMissingReference,
				fileIndex:          fileIndex,
				declIndex:          declIndex,
			})
		}
	}

	env, err := buildOverlayEnv()
	if err != nil {
		return nil, fmt.Errorf("invalid overlay rulebook: build CEL env: %w", err)
	}

	for i := range allRules {
		if err := compileRule(env, &allRules[i]); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(allRules, func(i, j int) bool {
		a, b := allRules[i], allRules[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.fileIndex != b.fileIndex {
			return a.fileIndex < b.fileIndex
		}
		if a.declIndex != b.declIndex {
			return a.declIndex < b.declIndex
		}
		return a.ID < b.ID
	})

	sum := sha256.Sum256(hashBuf.Bytes())

	return &Rulebook{
		OverlayRulebookHash: hex.EncodeToString(sum[:]),
		constants:           mergedConsts,
		rules:               allRules,
	}, nil
}

// normalizeConst mirrors engine.LoadRulebook: integer YAML values are
// promoted to float64 so CEL double arithmetic works regardless of
// whether the author wrote `9` or `9.0`.
func normalizeConst(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint64:
		return float64(x)
	default:
		return v
	}
}

// compileRule compiles the rule's `when` and `formula` expressions
// against the overlay CEL env and stores the programs on the rule.
// `when == ""` is treated as the literal `true` (rule always matches),
// per the epic edge case. `formula == ""` is legal only for mode
// "block"; validateRawRule already rejected the other modes.
func compileRule(env *cel.Env, r *overlayRule) error {
	whenExpr := r.When
	if strings.TrimSpace(whenExpr) == "" {
		whenExpr = "true"
	}
	prog, outType, err := compileExpr(env, whenExpr)
	if err != nil {
		return fmt.Errorf("invalid overlay rulebook: rule %q when %q: %w", r.ID, r.When, err)
	}
	if !outType.IsExactType(cel.BoolType) {
		return fmt.Errorf("invalid overlay rulebook: rule %q when must return bool, got %s", r.ID, outType)
	}
	r.whenProg = prog

	if r.Formula != "" {
		fprog, fout, err := compileExpr(env, r.Formula)
		if err != nil {
			return fmt.Errorf("invalid overlay rulebook: rule %q formula %q: %w", r.ID, r.Formula, err)
		}
		// Accept Double, Int, and DynType (dyn-typed maps make many
		// real formulas type to Dyn). Reject explicit Bool/String
		// outputs — those are author bugs and the eval-time number
		// guard upstream cannot help an overlay engine that hasn't
		// landed yet.
		if !fout.IsExactType(cel.DoubleType) &&
			!fout.IsExactType(cel.IntType) &&
			!fout.IsExactType(cel.DynType) {
			return fmt.Errorf("invalid overlay rulebook: rule %q formula must return a number, got %s", r.ID, fout)
		}
		r.formulaProg = fprog
	}
	return nil
}

func compileExpr(env *cel.Env, expr string) (cel.Program, *cel.Type, error) {
	ast, iss := env.Compile(expr)
	if iss.Err() != nil {
		return nil, nil, iss.Err()
	}
	prog, err := env.Program(ast)
	if err != nil {
		return nil, nil, err
	}
	return prog, ast.OutputType(), nil
}

// buildOverlayEnv constructs the CEL environment that every overlay
// rule's `when` and `formula` compiles against. Fact namespaces match
// the schema described in the epic:
//
//   - account: account-level facts (current_equity, sod_equity, ...).
//   - position: per-position facts (long_market_value, kind, symbol).
//   - security: mirror of SecurityFacts.
//   - group: group-scope facts (gross_market_value, position_count).
//   - constants: the merged rule-constants map.
//
// Every namespace is declared as map<string, dyn> so authors can mix
// numeric and string fields without per-field declarations; compile-
// time type checking still catches the cases that matter (when must
// return bool; formula must return a number).
func buildOverlayEnv() (*cel.Env, error) {
	factMap := cel.MapType(cel.StringType, cel.DynType)
	return cel.NewEnv(
		cel.Variable("account", factMap),
		cel.Variable("position", factMap),
		cel.Variable("security", factMap),
		cel.Variable("group", factMap),
		cel.Variable("constants", factMap),
	)
}
