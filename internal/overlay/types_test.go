package overlay

import (
	"errors"
	"testing"

	"margincalc/internal/account"
)

func TestZeroValuesAreUsable(t *testing.T) {
	var hr HouseRequirement
	if hr.Components != nil {
		t.Errorf("expected nil Components on zero HouseRequirement, got %v", hr.Components)
	}
	if hr.Audit.Entries != nil {
		t.Errorf("expected nil Audit.Entries on zero HouseRequirement, got %v", hr.Audit.Entries)
	}
}

func TestSecKeyString(t *testing.T) {
	got := SecKey{Symbol: "AAPL", Venue: "listed"}.String()
	if got != "AAPL@listed" {
		t.Errorf("SecKey.String() = %q, want %q", got, "AAPL@listed")
	}
}

func TestEngineEvaluateNotImplemented(t *testing.T) {
	var e Engine
	_, err := e.Evaluate(account.Account{}, account.AccountSnapshot{}, ReferenceData{})
	if !errors.Is(err, errNotImplemented) {
		t.Errorf("Engine.Evaluate err = %v, want errNotImplemented", err)
	}
}
