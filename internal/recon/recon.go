// Package recon runs the margin engine against a batch of positions for
// which a separate source-of-truth (typically the firm's existing vendor)
// has already produced a margin number, and reports every divergence.
//
// Two CSV inputs:
//
//	positions.csv  — one row per position with U, class, account_type,
//	                 phase, and the vendor's requirement
//	legs.csv       — one row per leg, keyed by position_id
//
// One CSV output:
//
//	diff.csv       — one row per position with engine vs. vendor numbers
//	                 and a verdict
//
// The harness is intentionally schema-driven and forgiving: unknown columns
// are ignored, missing optional columns default to zero. The point is to
// turn whatever the vendor exports into something the engine can consume
// with one new column-mapping function per export format, not a rewrite.
package recon

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"margincalc/internal/engine"
)

// Verdict classifies how the engine's output compares to the vendor's.
type Verdict string

const (
	VerdictMatch  Verdict = "MATCH"
	VerdictDiff   Verdict = "DIFF"
	VerdictNoRule Verdict = "NO_RULE"
	VerdictError  Verdict = "ERROR"
)

// PositionRow is one row of positions.csv after parsing.
type PositionRow struct {
	ID                string
	U                 float64
	Class             string
	Lev               float64
	AccountType       engine.AccountType
	Phase             engine.Phase
	VendorRequirement float64
}

// DiffRow is one row of the output diff.csv.
type DiffRow struct {
	PositionID        string
	VendorRequirement float64
	EngineRequirement float64
	EngineProceeds    float64
	EngineCashCall    float64
	Delta             float64 // engine - vendor
	AbsDelta          float64
	Verdict           Verdict
	RuleID            string
	Error             string
}

// Summary buckets the DiffRow set for a quick human read of the run.
type Summary struct {
	Total  int
	Match  int
	Diff   int
	NoRule int
	Error  int
	// Buckets of |delta| for the DIFF rows.
	DiffUnder1    int
	DiffUnder100  int
	DiffUnder1000 int
	DiffOver1000  int
}

// Options controls comparison behavior.
type Options struct {
	// Tolerance below which a delta is treated as MATCH. Set to 0 for exact.
	Tolerance float64
}

// LoadPositions reads positions.csv and legs.csv and returns the merged
// position set, in the order positions.csv specifies. Positions whose ID
// appears in positions.csv but has no leg rows in legs.csv come back with
// an empty leg slice — Evaluate will then no-match them, which is the
// correct outcome (better than silently dropping them).
func LoadPositions(positionsPath, legsPath string) ([]PositionRow, []engine.Position, error) {
	rows, err := readCSV(positionsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("positions: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("positions: file has no data rows")
	}
	header := normalize(rows[0].Record)
	pIdx := indexer(header)

	posRows := make([]PositionRow, 0, len(rows)-1)
	for _, r := range rows[1:] {
		p, err := parsePositionRow(r.Record, pIdx)
		if err != nil {
			return nil, nil, fmt.Errorf("positions line %d: %w", r.Line, err)
		}
		posRows = append(posRows, p)
	}

	// Legs grouped by position_id.
	legsByID, err := loadLegs(legsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("legs: %w", err)
	}

	positions := make([]engine.Position, len(posRows))
	for i, pr := range posRows {
		positions[i] = engine.Position{
			U:     pr.U,
			Class: pr.Class,
			Lev:   pr.Lev,
			Legs:  legsByID[pr.ID],
		}
	}
	return posRows, positions, nil
}

// Run feeds each (PositionRow, Position) pair through the engine and
// classifies the result against the vendor number.
func Run(rb *engine.Rulebook, rows []PositionRow, positions []engine.Position, opts Options) []DiffRow {
	if len(rows) != len(positions) {
		panic("recon.Run: rows and positions length mismatch")
	}
	out := make([]DiffRow, len(rows))
	for i, pr := range rows {
		d := DiffRow{
			PositionID:        pr.ID,
			VendorRequirement: pr.VendorRequirement,
		}
		res, err := rb.Evaluate(positions[i], pr.AccountType, pr.Phase)
		switch {
		case err != nil && strings.Contains(err.Error(), "no rule matched"):
			d.Verdict = VerdictNoRule
		case err != nil:
			d.Verdict = VerdictError
			d.Error = err.Error()
		default:
			d.EngineRequirement = res.Requirement
			d.EngineProceeds = res.AppliedProceeds
			d.EngineCashCall = res.CashCall
			d.RuleID = res.RuleID
			d.Delta = res.Requirement - pr.VendorRequirement
			d.AbsDelta = math.Abs(d.Delta)
			if d.AbsDelta <= opts.Tolerance {
				d.Verdict = VerdictMatch
			} else {
				d.Verdict = VerdictDiff
			}
		}
		out[i] = d
	}
	return out
}

// Summarize buckets the diff rows for at-a-glance reporting.
func Summarize(diffs []DiffRow) Summary {
	s := Summary{Total: len(diffs)}
	for _, d := range diffs {
		switch d.Verdict {
		case VerdictMatch:
			s.Match++
		case VerdictDiff:
			s.Diff++
			switch {
			case d.AbsDelta < 1:
				s.DiffUnder1++
			case d.AbsDelta < 100:
				s.DiffUnder100++
			case d.AbsDelta < 1000:
				s.DiffUnder1000++
			default:
				s.DiffOver1000++
			}
		case VerdictNoRule:
			s.NoRule++
		case VerdictError:
			s.Error++
		}
	}
	return s
}

// WriteDiff writes the diff rows to a CSV at path. Caller can sort first
// if they want them grouped by verdict or by abs(delta) — we keep them
// in input order so position_id is easy to grep against.
func WriteDiff(path string, diffs []DiffRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"position_id", "vendor_requirement", "engine_requirement",
		"engine_proceeds", "engine_cash_call", "delta", "abs_delta",
		"verdict", "rule_id", "error",
	}); err != nil {
		return err
	}
	for _, d := range diffs {
		if err := w.Write([]string{
			d.PositionID,
			ftoa(d.VendorRequirement),
			ftoa(d.EngineRequirement),
			ftoa(d.EngineProceeds),
			ftoa(d.EngineCashCall),
			ftoa(d.Delta),
			ftoa(d.AbsDelta),
			string(d.Verdict),
			d.RuleID,
			d.Error,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return nil
}

// FormatSummary renders a Summary as a small human-readable block.
func FormatSummary(s Summary) string {
	pct := func(n int) string {
		if s.Total == 0 {
			return "0.0%"
		}
		return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(s.Total))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Reconciliation summary (%d positions)\n", s.Total)
	fmt.Fprintf(&b, "  MATCH    %5d  %s\n", s.Match, pct(s.Match))
	fmt.Fprintf(&b, "  DIFF     %5d  %s\n", s.Diff, pct(s.Diff))
	fmt.Fprintf(&b, "    |Δ| < $1       %5d\n", s.DiffUnder1)
	fmt.Fprintf(&b, "    |Δ| < $100     %5d\n", s.DiffUnder100)
	fmt.Fprintf(&b, "    |Δ| < $1000    %5d\n", s.DiffUnder1000)
	fmt.Fprintf(&b, "    |Δ| >= $1000   %5d\n", s.DiffOver1000)
	fmt.Fprintf(&b, "  NO_RULE  %5d  %s\n", s.NoRule, pct(s.NoRule))
	fmt.Fprintf(&b, "  ERROR    %5d  %s\n", s.Error, pct(s.Error))
	return b.String()
}

// --- internals -------------------------------------------------------------

func loadLegs(path string) (map[string][]engine.Leg, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return map[string][]engine.Leg{}, nil
	}
	header := normalize(rows[0].Record)
	idx := indexer(header)
	type indexed struct {
		i   int
		leg engine.Leg
	}
	byID := map[string][]indexed{}
	for _, row := range rows[1:] {
		r := row.Record
		id := idx.str(r, "position_id")
		if id == "" {
			return nil, fmt.Errorf("legs line %d: missing position_id", row.Line)
		}
		leg := engine.Leg{
			Side:             engine.Side(idx.str(r, "side")),
			Kind:             engine.Kind(idx.str(r, "kind")),
			OptionType:       idx.str(r, "option_type"),
			Style:            idx.str(r, "style"),
			Venue:            idx.str(r, "venue"),
			SettleStyle:      idx.str(r, "settle_style"),
			Expiration:       idx.str(r, "expiration"),
			Underlying:       idx.str(r, "underlying"),
			BrokerGuaranteed: idx.bool(r, "broker_guaranteed"),
			TracksIndex:      idx.str(r, "tracks_index"),
			Leveraged:        idx.bool(r, "leveraged"),
		}
		// qty and mult are load-bearing: a leg with qty=0 or mult=0
		// silently zeroes every per-leg requirement. Require them
		// explicitly so an empty cell errors instead of producing a
		// degenerate leg. Other float fields (k_equivalent, shares,
		// short_sale_proceeds, …) are slot-conditional and stay optional.
		if v, err := idx.requiredFloat(r, "qty"); err != nil {
			return nil, fmt.Errorf("legs line %d: %w", row.Line, err)
		} else {
			leg.Qty = v
		}
		if v, err := idx.requiredFloat(r, "mult"); err != nil {
			return nil, fmt.Errorf("legs line %d: %w", row.Line, err)
		} else {
			leg.Mult = v
		}
		floatFields := []struct {
			key string
			dst *float64
		}{
			{"k", &leg.K},
			{"p", &leg.P},
			{"p0", &leg.P0},
			{"time_to_expiration_months", &leg.TimeToExpirationMonths},
			{"shares", &leg.Shares},
			{"short_sale_proceeds", &leg.ShortSaleProceeds},
			{"sale_price", &leg.SalePrice},
			{"price", &leg.Price},
			{"k_equivalent", &leg.KEquivalent},
		}
		for _, ff := range floatFields {
			v, err := idx.float(r, ff.key)
			if err != nil {
				return nil, fmt.Errorf("legs line %d: %w", row.Line, err)
			}
			*ff.dst = v
		}
		legIdxF, err := idx.float(r, "leg_index")
		if err != nil {
			return nil, fmt.Errorf("legs line %d: %w", row.Line, err)
		}
		legIdx := int(legIdxF)
		byID[id] = append(byID[id], indexed{i: legIdx, leg: leg})
	}
	out := map[string][]engine.Leg{}
	for id, legs := range byID {
		sort.Slice(legs, func(i, j int) bool { return legs[i].i < legs[j].i })
		flat := make([]engine.Leg, len(legs))
		for i, ix := range legs {
			flat[i] = ix.leg
		}
		out[id] = flat
	}
	return out, nil
}

func parsePositionRow(r []string, idx indexerMap) (PositionRow, error) {
	p := PositionRow{
		ID:          idx.str(r, "position_id"),
		Class:       idx.str(r, "class"),
		AccountType: engine.AccountType(strings.ToLower(idx.str(r, "account_type"))),
		Phase:       engine.Phase(strings.ToLower(idx.str(r, "phase"))),
	}
	var err error
	if p.U, err = idx.float(r, "u"); err != nil {
		return p, err
	}
	if p.Lev, err = idx.float(r, "lev"); err != nil {
		return p, err
	}
	if p.VendorRequirement, err = idx.float(r, "vendor_requirement"); err != nil {
		return p, err
	}
	if p.ID == "" {
		return p, fmt.Errorf("missing position_id")
	}
	if p.AccountType == "" {
		p.AccountType = engine.MarginAccount
	}
	if p.Phase == "" {
		p.Phase = engine.Initial
	}
	return p, nil
}

// csvRow pairs a CSV record with its original 1-based line number in the
// source file. Empty rows are skipped, so the slice index alone cannot
// reproduce the file line; callers that report errors must use Line.
type csvRow struct {
	Line   int
	Record []string
}

func readCSV(path string) ([]csvRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(stripBOM(f))
	r.FieldsPerRecord = -1 // tolerate ragged rows; we look up by header name
	r.TrimLeadingSpace = true
	var rows []csvRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// FieldPos(0) returns the 1-based line of the most-recently-read
		// record's first field — robust against blank lines that we skip
		// below and against any future quoted multi-line fields.
		line, _ := r.FieldPos(0)
		if allEmpty(rec) {
			continue
		}
		rows = append(rows, csvRow{Line: line, Record: rec})
	}
	return rows, nil
}

// stripBOM drops a leading UTF-8 BOM (\xEF\xBB\xBF) so Excel-exported CSVs
// don't poison the first header cell.
func stripBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	b, err := br.Peek(3)
	if err == nil && len(b) == 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br
}

func allEmpty(rec []string) bool {
	for _, s := range rec {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

func normalize(h []string) []string {
	out := make([]string, len(h))
	for i, s := range h {
		out[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

type indexerMap map[string]int

func indexer(header []string) indexerMap {
	m := indexerMap{}
	for i, h := range header {
		m[h] = i
	}
	return m
}

func (m indexerMap) str(row []string, key string) string {
	i, ok := m[key]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func (m indexerMap) float(row []string, key string) (float64, error) {
	s := m.str(row, key)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("field %q: %w", key, err)
	}
	return v, nil
}

// requiredFloat is like float, but rejects empty strings. Use it for fields
// where 0 has a meaningful effect on the engine (qty, mult): a silent
// zero-fallback would produce a leg that contributes nothing to the
// requirement, which looks like a quiet MATCH for shorts the engine never
// charged. Returns an error naming the field so the caller can wrap it
// with row context.
func (m indexerMap) requiredFloat(row []string, key string) (float64, error) {
	s := m.str(row, key)
	if s == "" {
		return 0, fmt.Errorf("field %q: required but empty", key)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("field %q: %w", key, err)
	}
	return v, nil
}

func (m indexerMap) bool(row []string, key string) bool {
	s := strings.ToLower(m.str(row, key))
	return s == "true" || s == "1" || s == "yes" || s == "y"
}

func ftoa(f float64) string {
	if f == 0 {
		return ""
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}
