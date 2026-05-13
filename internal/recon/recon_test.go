package recon

import (
	"path/filepath"
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
