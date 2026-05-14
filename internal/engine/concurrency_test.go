package engine

import (
	"sync"
	"testing"
)

// TestConcurrentEvaluate_MinFieldsRule fires many goroutines at a position that
// triggers a rule using requires.min_fields (covered_call). LoadRulebook must
// pre-compile every gte expression; Evaluate must only read rb.progs. Run with
// -race to verify the "Rulebook is concurrent-safe" CLAUDE.md invariant.
func TestConcurrentEvaluate_MinFieldsRule(t *testing.T) {
	rb, err := LoadRulebook(rulesPath)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	pos := Position{
		U:     92.38,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 7.0, P0: 7.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Long, Kind: StockKind, Shares: 100, Underlying: "XYZ"},
		},
	}

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			res, err := rb.Evaluate(pos, MarginAccount, Initial)
			if err != nil {
				errs <- err
				return
			}
			if res.RuleID != "covered_call" {
				errs <- &concurrentMatchErr{got: res.RuleID}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent Evaluate: %v", e)
	}
}

type concurrentMatchErr struct{ got string }

func (e *concurrentMatchErr) Error() string {
	return "matched " + e.got + ", want covered_call"
}
