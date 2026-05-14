package recon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"margincalc/internal/engine"
)

func TestRecon_endToEnd(t *testing.T) {
	rb, err := engine.LoadRulebook(filepath.Join("..", "..", "rules", "cboe_baseline.yaml"))
	if err != nil {
		t.Fatalf("load rulebook: %v", err)
	}
	rows, positions, err := LoadPositions("testdata/positions.csv", "testdata/legs.csv")
	if err != nil {
		t.Fatalf("load positions: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d position rows, want 5", len(rows))
	}

	diffs := Run(rb, rows, positions, Options{Tolerance: 0.01})

	// Index by position_id so the assertions read clearly.
	byID := map[string]DiffRow{}
	for _, d := range diffs {
		byID[d.PositionID] = d
	}

	// short_call: engine yields 3410 vs vendor 3410 → MATCH
	if v := byID["POS_short_call"].Verdict; v != VerdictMatch {
		t.Errorf("POS_short_call verdict %s, want MATCH (Δ=%.2f)", v, byID["POS_short_call"].Delta)
	}
	// vertical: engine yields 880 vs vendor 880 → MATCH
	if v := byID["POS_vertical"].Verdict; v != VerdictMatch {
		t.Errorf("POS_vertical verdict %s, want MATCH (Δ=%.2f)", v, byID["POS_vertical"].Delta)
	}
	// short_put OTM: engine yields 1000 (200 premium + 800 basic) vs vendor 1000 → MATCH
	if v := byID["POS_short_put"].Verdict; v != VerdictMatch {
		t.Errorf("POS_short_put verdict %s, want MATCH (Δ=%.2f)", v, byID["POS_short_put"].Delta)
	}
	// unmatched ETN long-dated → NO_RULE (class-scope guard refuses)
	if v := byID["POS_unmatched"].Verdict; v != VerdictNoRule {
		t.Errorf("POS_unmatched verdict %s, want NO_RULE", v)
	}
	// vendor_wrong: same position as short_call but vendor says 2000 → DIFF of $1410
	d := byID["POS_vendor_wrong"]
	if d.Verdict != VerdictDiff {
		t.Errorf("POS_vendor_wrong verdict %s, want DIFF", d.Verdict)
	}
	if d.Delta < 1409 || d.Delta > 1411 {
		t.Errorf("POS_vendor_wrong delta %.2f, want ~1410", d.Delta)
	}

	// Summary buckets sanity-check.
	s := Summarize(diffs)
	if s.Total != 5 {
		t.Errorf("total %d, want 5", s.Total)
	}
	if s.Match != 3 {
		t.Errorf("match %d, want 3", s.Match)
	}
	if s.Diff != 1 {
		t.Errorf("diff %d, want 1", s.Diff)
	}
	if s.NoRule != 1 {
		t.Errorf("no_rule %d, want 1", s.NoRule)
	}
	if s.DiffOver1000 != 1 {
		t.Errorf("diff_over_1000 %d, want 1", s.DiffOver1000)
	}

	// WriteDiff should round-trip without error against a temp file.
	out := filepath.Join(t.TempDir(), "diff.csv")
	if err := WriteDiff(out, diffs); err != nil {
		t.Fatalf("WriteDiff: %v", err)
	}
}

// Engine validation errors must not be silently bucketed as NO_RULE. The
// classifier's "no rule matched" substring check is the contract that makes
// this work — if a future engine change rewords validation errors to
// include that phrase, this test catches the regression here rather than
// in production where a validation bug would masquerade as "vendor has a
// position our engine doesn't yet handle."
func TestRecon_validationErrorIsNotNoRule(t *testing.T) {
	rb, err := engine.LoadRulebook(filepath.Join("..", "..", "rules", "cboe_baseline.yaml"))
	if err != nil {
		t.Fatalf("load rulebook: %v", err)
	}
	// U=0 is rejected by validatePosition before any rule matching.
	rows := []PositionRow{{
		ID: "POS_invalid", U: 0, Class: "equity", Lev: 1.0,
		AccountType: engine.MarginAccount, Phase: engine.Initial,
		VendorRequirement: 1000.00,
	}}
	positions := []engine.Position{{
		U: 0, Class: "equity",
		Legs: []engine.Leg{
			{Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
				K: 80, P: 2, P0: 2, Qty: 1, Mult: 100},
		},
	}}
	diffs := Run(rb, rows, positions, Options{})
	if len(diffs) != 1 {
		t.Fatalf("got %d diffs, want 1", len(diffs))
	}
	if diffs[0].Verdict != VerdictError {
		t.Errorf("verdict=%s, want ERROR", diffs[0].Verdict)
	}
	if !strings.Contains(diffs[0].Error, "invalid position") {
		t.Errorf("error %q does not contain 'invalid position' prefix", diffs[0].Error)
	}
}

// Blank lines before a malformed row must not shift the reported file line:
// the error message has to point at the actual line in the source CSV, not
// the index of the row among the non-blank rows. This is what makes recon
// errors greppable against the file the user is staring at.
func TestLoadPositions_blankLinesPreserveLineNumber(t *testing.T) {
	dir := t.TempDir()
	posPath := filepath.Join(dir, "positions.csv")
	legsPath := filepath.Join(dir, "legs.csv")

	// Two leading blank lines, then header on line 3, then a row on line 4
	// whose `u` value is unparseable. Without line-preserving readCSV the
	// error would say "row 2".
	pos := "\n\nposition_id,u,class,lev,account_type,phase,vendor_requirement\n" +
		"POS_bad,not_a_number,equity,1.0,margin,initial,1000\n"
	if err := os.WriteFile(posPath, []byte(pos), 0o644); err != nil {
		t.Fatalf("write positions: %v", err)
	}
	if err := os.WriteFile(legsPath, []byte("position_id,leg_index,side,kind,qty,mult\n"), 0o644); err != nil {
		t.Fatalf("write legs: %v", err)
	}

	_, _, err := LoadPositions(posPath, legsPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "line 4") {
		t.Errorf("error %q does not reference 'line 4' (the actual file line of the bad row)", err.Error())
	}
	// Also assert the buggy old shape is not present.
	if strings.Contains(err.Error(), "row 2") {
		t.Errorf("error %q still uses pre-fix 'row 2' wording", err.Error())
	}
}

// qty=0 (or empty) is the silent failure mode that prompted this guard: a
// leg with qty=0 contributes zero to the engine's requirement, so the
// recon harness would report MATCH for any vendor number when in fact the
// data is malformed. requiredFloat must reject the empty cell.
func TestLoadPositions_emptyQtyRejected(t *testing.T) {
	dir := t.TempDir()
	posPath := filepath.Join(dir, "positions.csv")
	legsPath := filepath.Join(dir, "legs.csv")

	pos := "position_id,u,class,lev,account_type,phase,vendor_requirement\n" +
		"POS_x,100,equity,1.0,margin,initial,1000\n"
	if err := os.WriteFile(posPath, []byte(pos), 0o644); err != nil {
		t.Fatalf("write positions: %v", err)
	}
	// Empty qty cell on the data row — everything else is well-formed.
	legs := "position_id,leg_index,side,kind,option_type,k,p,p0,qty,mult\n" +
		"POS_x,0,short,option,call,120,8.40,8.40,,100\n"
	if err := os.WriteFile(legsPath, []byte(legs), 0o644); err != nil {
		t.Fatalf("write legs: %v", err)
	}

	_, _, err := LoadPositions(posPath, legsPath)
	if err == nil {
		t.Fatalf("expected error on empty qty, got nil")
	}
	if !strings.Contains(err.Error(), "qty") {
		t.Errorf("error %q does not mention the offending field 'qty'", err.Error())
	}
}
