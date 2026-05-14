// Package overlay is the Layer 3 house-overlay engine. It consumes a
// Layer-2 account.AccountSnapshot plus reference data and emits a
// HouseRequirement: the customer-facing house number, attributed as a
// bridge from the regulatory baseline to the final requirement with an
// audit trail.
//
// Layering: overlay sits above internal/account (Layer 2) and below any
// future regime orchestrator. Layer 3 is one regime among future engines
// (TIMS, SPAN, shock); those will live behind a future internal/regime
// orchestrator and pick which engine to run per account/strategy.
//
// Sign convention: SMV magnitudes inherited from internal/account are
// positive. Overlay components use the same convention so deltas added
// to requirement values remain in dollars-positive.
//
// Single-currency MVP: every input (Account, AccountSnapshot,
// ReferenceData) is assumed to be denominated in a single currency.
// HouseRequirement.Currency is copied from the snapshot and no FX
// conversion happens inside the engine. Multi-currency accounts are out
// of scope until the Layer-2 aggregator supports them.
//
// Reconciliation against vendor house numbers is deferred to the API
// layer (cmd/recon and a future vendor adapter), not done inside the
// engine.
//
// Working example: rules/house_overlay.example.yaml ships a runnable
// overlay rulebook covering position-scope floor/block modes and
// group-scope max — load this for a working example of the schema.
package overlay
