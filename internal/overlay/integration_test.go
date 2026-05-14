package overlay

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"margincalc/internal/account"
	"margincalc/internal/engine"
)

// exampleOverlayPath / baselineRulesPath are the relative paths used by
// every integration test in this file. The repository convention,
// mirrored from internal/engine and internal/account, is that
// package-level tests load YAML from `../../rules/`.
const (
	exampleOverlayPath = "../../rules/house_overlay.example.yaml"
	baselineRulesPath  = "../../rules/cboe_baseline.yaml"
)

func loadIntegrationOverlay(t *testing.T) *Rulebook {
	t.Helper()
	rb, err := LoadRulebook(exampleOverlayPath)
	if err != nil {
		t.Fatalf("LoadRulebook(%s): %v", exampleOverlayPath, err)
	}
	return rb
}

func loadIntegrationEngineRulebook(t *testing.T) *engine.Rulebook {
	t.Helper()
	rb, err := engine.LoadRulebook(baselineRulesPath)
	if err != nil {
		t.Fatalf("engine.LoadRulebook(%s): %v", baselineRulesPath, err)
	}
	return rb
}

// integrationStock builds a single-leg long-stock position with a
// caller-controlled venue, so D4 tests can construct (SYM, listed) and
// (SYM, otc) positions in the same account.
func integrationStock(id, symbol, venue string, side engine.Side, shares, price float64) account.AccountPosition {
	return account.AccountPosition{
		ID: id,
		Position: engine.Position{
			U:     price,
			Class: "equity",
			Lev:   1,
			Legs: []engine.Leg{{
				Side:       side,
				Kind:       engine.StockKind,
				Shares:     shares,
				Underlying: symbol,
				Venue:      venue,
				Mult:       1,
			}},
		},
	}
}

func findComponent(comps []HouseComponent, ruleID, positionID string) (HouseComponent, bool) {
	for _, c := range comps {
		if c.RuleID == ruleID && c.PositionID == positionID {
			return c, true
		}
	}
	return HouseComponent{}, false
}

func findGroupComponent(comps []HouseComponent, ruleID, groupKey string) (HouseComponent, bool) {
	for _, c := range comps {
		if c.RuleID == ruleID && c.GroupKey == groupKey {
			return c, true
		}
	}
	return HouseComponent{}, false
}

// TestIntegration_ExampleYAMLLoads is the smoke test: the shipped
// example file must parse and compile via overlay.LoadRulebook from the
// package's working directory using the repo-standard `../../rules/`
// path. The integration scenario below also exercises this load path,
// but a dedicated test pins the loader contract independently of the
// scenario assertions.
func TestIntegration_ExampleYAMLLoads(t *testing.T) {
	rb, err := LoadRulebook(exampleOverlayPath)
	if err != nil {
		t.Fatalf("LoadRulebook(%s): %v", exampleOverlayPath, err)
	}
	if rb == nil {
		t.Fatal("LoadRulebook returned nil rulebook")
	}
	if rb.OverlayRulebookHash == "" {
		t.Error("expected non-empty OverlayRulebookHash")
	}
	if rb.ruleCount() == 0 {
		t.Error("expected at least one compiled rule from the example file")
	}
}

// TestIntegration_Layer1Plus2Plus3_Bridge wires the full stack:
// Account → AggregateWithRulebook (Layer 1+2) → Engine.Evaluate
// (Layer 3) → HouseRequirement. The scenario exercises every overlay
// emission path the example YAML defines:
//
//   - position-scope max (low_price_long_floor) on two positions sharing
//     an underlying;
//   - group-scope max (single_name_concentration) over those two
//     positions, demonstrating the D2 baseline-sum floor;
//   - position-scope block (non_marginable_block) emitting a violation
//     without moving HouseRequirement;
//   - a stock-like position with no ReferenceData entry, exercising the
//     D3 missing-ref fallback warning;
//   - a venue-keyed reference-data row (PNKY@otc) so the D4 SecKey
//     {Symbol, Venue} shape gets touched by the main scenario too.
func TestIntegration_Layer1Plus2Plus3_Bridge(t *testing.T) {
	engineRB := loadIntegrationEngineRulebook(t)
	overlayRB := loadIntegrationOverlay(t)

	// pA: low-priced AAPL long (triggers low_price_long_floor).
	pA := integrationStock("pA", "AAPL", "listed", engine.Long, 1000, 4.50)
	// pB: high-priced MSFT long (no overlay fires).
	pB := integrationStock("pB", "MSFT", "listed", engine.Long, 50, 400.0)
	// pC: non-marginable position on OTC (triggers non_marginable_block
	// and exercises the (SYMBOL, otc) reference-data key shape).
	pC := integrationStock("pC", "PNKY", "otc", engine.Long, 100, 50.0)
	// pD: large second AAPL position to push the AAPL group's gross MV
	// above concentration_threshold * current_equity.
	pD := integrationStock("pD", "AAPL", "listed", engine.Long, 10000, 4.50)
	// pE: missing reference-data entry → D3 fallback warning.
	pE := integrationStock("pE", "MISS", "listed", engine.Long, 10, 30.0)

	acct := account.Account{
		ID:          "ACCT-INT",
		AccountType: engine.MarginAccount,
		Phase:       engine.Maintenance,
		AsOf:        time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		Currency:    "USD",
		SODEquity:   50000,
		CashBalance: 50000,
		Positions:   []account.AccountPosition{pA, pB, pC, pD, pE},
	}

	snap, err := account.AggregateWithRulebook(engineRB, acct)
	if err != nil {
		t.Fatalf("AggregateWithRulebook: %v", err)
	}
	// Long-stock positions don't match any baseline rule today → NoRule
	// classification, TotalRequirement remains 0. The integration test
	// pins that assumption so a future baseline-rule addition (which
	// would change the bridge math) surfaces here.
	if snap.TotalRequirement != 0 {
		t.Fatalf("baseline TotalRequirement = %v, want 0 (no long-stock baseline rule yet)", snap.TotalRequirement)
	}

	ref := ReferenceData{Securities: map[SecKey]SecurityFacts{
		{Symbol: "AAPL", Venue: "listed"}: {
			Symbol: "AAPL", Venue: "listed",
			InstrumentKind: "stock", LastPrice: 4.50, Marginable: true,
		},
		{Symbol: "MSFT", Venue: "listed"}: {
			Symbol: "MSFT", Venue: "listed",
			InstrumentKind: "stock", LastPrice: 400.0, Marginable: true,
		},
		{Symbol: "PNKY", Venue: "otc"}: {
			Symbol: "PNKY", Venue: "otc",
			InstrumentKind: "stock", LastPrice: 50.0, Marginable: false,
		},
		// MISS@listed intentionally omitted (D3).
	}}

	eng := &Engine{Rulebook: overlayRB}
	hr, err := eng.Evaluate(acct, snap, ref)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// --- baseline / audit invariants -----------------------------------------

	if hr.BaselineRequirement != snap.TotalRequirement {
		t.Errorf("BaselineRequirement = %v, want %v", hr.BaselineRequirement, snap.TotalRequirement)
	}
	if hr.Audit.OverlayRulebookHash == "" {
		t.Error("Audit.OverlayRulebookHash is empty")
	}
	if hr.Audit.OverlayRulebookHash != overlayRB.OverlayRulebookHash {
		t.Errorf("Audit.OverlayRulebookHash = %q, want %q",
			hr.Audit.OverlayRulebookHash, overlayRB.OverlayRulebookHash)
	}

	// --- low_price_long_floor on pA ------------------------------------------

	compA, ok := findComponent(hr.Components, "low_price_long_floor", "pA")
	if !ok {
		t.Fatalf("missing low_price_long_floor component for pA; have %+v", hr.Components)
	}
	if compA.Mode != "max" {
		t.Errorf("pA component Mode = %q, want max", compA.Mode)
	}
	wantPA := 1000.0 * 2.50
	if compA.OverlayAmount != wantPA {
		t.Errorf("pA OverlayAmount = %v, want %v", compA.OverlayAmount, wantPA)
	}
	if !compA.Applied {
		t.Errorf("pA Applied = false; baseline=%v overlay=%v", compA.BaselineAmount, compA.OverlayAmount)
	}
	if compA.Delta != wantPA {
		t.Errorf("pA Delta = %v, want %v", compA.Delta, wantPA)
	}

	// --- low_price_long_floor on pD ------------------------------------------

	compD, ok := findComponent(hr.Components, "low_price_long_floor", "pD")
	if !ok {
		t.Fatalf("missing low_price_long_floor component for pD; have %+v", hr.Components)
	}
	wantPD := 10000.0 * 2.50
	if compD.Delta != wantPD || !compD.Applied {
		t.Errorf("pD component = %+v; want Delta=%v Applied=true", compD, wantPD)
	}

	// --- non_marginable_block on pC ------------------------------------------

	var blockComps []HouseComponent
	for _, c := range hr.Components {
		if c.RuleID == "non_marginable_block" {
			blockComps = append(blockComps, c)
		}
	}
	if len(blockComps) != 1 {
		t.Fatalf("non_marginable_block components = %d, want 1; got %+v", len(blockComps), blockComps)
	}
	bc := blockComps[0]
	if bc.PositionID != "pC" || bc.Mode != "block" || bc.Delta != 0 || !bc.Applied {
		t.Errorf("block component = %+v", bc)
	}
	var blockViolations []HouseViolation
	for _, v := range hr.Violations {
		if v.RuleID == "non_marginable_block" {
			blockViolations = append(blockViolations, v)
		}
	}
	if len(blockViolations) != 1 {
		t.Fatalf("non_marginable_block violations = %d, want 1; got %+v", len(blockViolations), blockViolations)
	}
	if blockViolations[0].PositionID != "pC" || blockViolations[0].Symbol != "PNKY" {
		t.Errorf("block violation = %+v", blockViolations[0])
	}

	// --- single_name_concentration on AAPL (group max, D2) -------------------

	gc, ok := findGroupComponent(hr.Components, "single_name_concentration", "AAPL")
	if !ok {
		t.Fatalf("missing single_name_concentration component for AAPL; have %+v", hr.Components)
	}
	if gc.Mode != "max" {
		t.Errorf("group component Mode = %q, want max", gc.Mode)
	}
	wantGroupOverlay := (1000.0*4.50 + 10000.0*4.50) * 0.30
	if gc.OverlayAmount != wantGroupOverlay {
		t.Errorf("AAPL group OverlayAmount = %v, want %v", gc.OverlayAmount, wantGroupOverlay)
	}
	// D2: BaselineAmount is the sum of per-position Layer-1 requirements
	// in the group. Both AAPL positions are NoRule → baseline sum is 0.
	if gc.BaselineAmount != 0 {
		t.Errorf("AAPL group BaselineAmount = %v, want 0 (Layer-1 sum)", gc.BaselineAmount)
	}
	if !gc.Applied || gc.Delta != wantGroupOverlay {
		t.Errorf("AAPL group component = %+v; want Applied=true Delta=%v", gc, wantGroupOverlay)
	}
	// Per-position baselines must appear in Evidence (D2 audit echo).
	for _, id := range []string{"pA", "pD"} {
		if _, ok := gc.Evidence["baseline."+id]; !ok {
			t.Errorf("AAPL group Evidence missing baseline.%s; have %v", id, gc.Evidence)
		}
	}

	// --- HouseRequirement = baseline + sum(applied component deltas) ---------

	var deltaSum float64
	for _, c := range hr.Components {
		if c.Applied {
			deltaSum += c.Delta
		}
	}
	if got, want := hr.HouseRequirement, hr.BaselineRequirement+deltaSum; got != want {
		t.Errorf("HouseRequirement = %v, want %v (baseline %v + applied deltas %v)",
			got, want, hr.BaselineRequirement, deltaSum)
	}

	// --- D3: missing reference data → one warning for pE --------------------

	var missWarns []string
	for _, w := range hr.Warnings {
		if strings.Contains(w, "MISS@listed") {
			missWarns = append(missWarns, w)
		}
	}
	if len(missWarns) != 1 {
		t.Errorf("expected exactly one MISS@listed missing-ref warning, got %v", hr.Warnings)
	}

	// --- determinism (#46): two consecutive calls return DeepEqual results ---

	hr2, err := eng.Evaluate(acct, snap, ref)
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if !reflect.DeepEqual(hr, hr2) {
		t.Errorf("two consecutive Evaluate calls disagree:\n hr1=%+v\n hr2=%+v", hr, hr2)
	}
}

// TestIntegration_VenueDistinctReferenceData_D4 pins the SecKey shape:
// two positions in the same underlying on different venues must look up
// their own SecurityFacts entries. The listed entry is below the
// low-price threshold (rule fires); the otc entry is above (rule does
// not fire). One Component on the listed position, none on the otc one,
// proves each lookup hit its venue-specific row.
func TestIntegration_VenueDistinctReferenceData_D4(t *testing.T) {
	overlayRB := loadIntegrationOverlay(t)

	pListed := integrationStock("p-listed", "UNI", "listed", engine.Long, 100, 4.0)
	pOTC := integrationStock("p-otc", "UNI", "otc", engine.Long, 100, 50.0)

	acct := account.Account{
		ID:          "ACCT-D4",
		AccountType: engine.MarginAccount,
		Phase:       engine.Maintenance,
		AsOf:        time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		Currency:    "USD",
		SODEquity:   100000,
		CashBalance: 100000,
		Positions:   []account.AccountPosition{pListed, pOTC},
	}

	evals := []account.PositionEvaluation{
		{PositionID: "p-listed"},
		{PositionID: "p-otc"},
	}
	snap, err := account.Aggregate(acct, evals)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	ref := ReferenceData{Securities: map[SecKey]SecurityFacts{
		{Symbol: "UNI", Venue: "listed"}: {
			Symbol: "UNI", Venue: "listed",
			InstrumentKind: "stock", LastPrice: 4.0, Marginable: true,
		},
		{Symbol: "UNI", Venue: "otc"}: {
			Symbol: "UNI", Venue: "otc",
			InstrumentKind: "stock", LastPrice: 50.0, Marginable: true,
		},
	}}

	eng := &Engine{Rulebook: overlayRB}
	hr, err := eng.Evaluate(acct, snap, ref)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	var listedComps, otcComps []HouseComponent
	for _, c := range hr.Components {
		if c.RuleID != "low_price_long_floor" {
			continue
		}
		switch c.PositionID {
		case "p-listed":
			listedComps = append(listedComps, c)
		case "p-otc":
			otcComps = append(otcComps, c)
		}
	}
	if len(listedComps) != 1 {
		t.Errorf("listed position should fire low_price_long_floor exactly once; got %v", listedComps)
	}
	if len(otcComps) != 0 {
		t.Errorf("otc position should NOT fire low_price_long_floor (last_price=50); got %v", otcComps)
	}
	if len(hr.Warnings) != 0 {
		t.Errorf("no missing-ref warnings expected when both venue entries are present; got %v", hr.Warnings)
	}
}

// TestIntegration_EmptyAccount_NoOverlayActivity guards the no-position
// path: Evaluate must return zero requirement, no components, no
// violations, and no warnings.
func TestIntegration_EmptyAccount_NoOverlayActivity(t *testing.T) {
	overlayRB := loadIntegrationOverlay(t)

	acct := account.Account{
		ID:          "ACCT-EMPTY",
		AccountType: engine.MarginAccount,
		Phase:       engine.Maintenance,
		AsOf:        time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		Currency:    "USD",
		SODEquity:   25000,
	}
	snap, err := account.Aggregate(acct, nil)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	eng := &Engine{Rulebook: overlayRB}
	hr, err := eng.Evaluate(acct, snap, ReferenceData{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if hr.BaselineRequirement != 0 || hr.HouseRequirement != 0 {
		t.Errorf("requirements = %v / %v, want 0 / 0", hr.BaselineRequirement, hr.HouseRequirement)
	}
	if len(hr.Components) != 0 {
		t.Errorf("Components = %v, want none", hr.Components)
	}
	if len(hr.Violations) != 0 {
		t.Errorf("Violations = %v, want none", hr.Violations)
	}
	if len(hr.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", hr.Warnings)
	}
}
