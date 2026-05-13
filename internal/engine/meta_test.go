package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestRulebook_NoRuleSpecificGoValidation guards the YAML-`requires` migration:
// once every rule's preconditions live in YAML, no top-level function in the
// engine package may take a `ruleID string` parameter to dispatch on rule
// identity. A reintroduced `validateRuleInputs`-style switch would defeat the
// epic's goal (#40) and silently re-enable rule-ID-keyed Go validation.
//
// The check parses every non-test .go file in this package's directory and
// asserts:
//   - no top-level function is literally named `validateRuleInputs`.
//   - no top-level free function takes a parameter named `ruleID`. The
//     RequireSpec interpreter is a method on *Rulebook, so it does not match;
//     small format helpers like requirePositive that take a ruleID for error
//     wording are free functions but only ever receive their ruleID from
//     validateRequirements, which is the allowed path — they're scoped out by
//     name in `helperAllowlist` below.
//
// If a future PR genuinely needs to reintroduce per-rule Go validation, the
// fix is to delete this guard *and* explain in the PR description why YAML
// `requires` cannot express the new check. Silently bypassing the guard by
// renaming the parameter defeats its purpose.
func TestRulebook_NoRuleSpecificGoValidation(t *testing.T) {
	helperAllowlist := map[string]struct{}{
		"requirePositive":         {},
		"requireSameContractSize": {},
		"requireExpirationSlots":  {},
		"requireSameStringField":  {},
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse engine package: %v", err)
	}

	var offenders []string
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if fn.Name.Name == "validateRuleInputs" {
					offenders = append(offenders, path+": function validateRuleInputs reintroduced")
					continue
				}
				if fn.Recv != nil {
					continue
				}
				if _, allowed := helperAllowlist[fn.Name.Name]; allowed {
					continue
				}
				if fn.Type.Params == nil {
					continue
				}
				for _, field := range fn.Type.Params.List {
					for _, name := range field.Names {
						if name.Name == "ruleID" {
							offenders = append(offenders, path+": function "+fn.Name.Name+" takes ruleID parameter")
						}
					}
				}
			}
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("rule-ID-keyed Go validation reintroduced (move the check to YAML `requires` instead):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
