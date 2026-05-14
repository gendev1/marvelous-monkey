// Command recon runs the margin engine against a CSV batch and writes a
// position-by-position diff against the vendor's numbers.
//
//	recon -rules rules/cboe_baseline.yaml \
//	      -positions positions.csv \
//	      -legs legs.csv \
//	      -out diff.csv \
//	      [-tolerance 0.01]
//
// Exit code is 0 on a clean run regardless of diff content; 1 on I/O or
// parsing failure. Reading the summary tells you whether the engine agrees
// with the vendor — that's a human / risk-team decision, not the CLI's.
package main

import (
	"flag"
	"fmt"
	"os"

	"margincalc/internal/engine"
	"margincalc/internal/recon"
)

func main() {
	var (
		rulesPath = flag.String("rules", "rules/cboe_baseline.yaml", "path to rules YAML")
		posPath   = flag.String("positions", "positions.csv", "path to positions.csv")
		legsPath  = flag.String("legs", "legs.csv", "path to legs.csv")
		outPath   = flag.String("out", "diff.csv", "path to write diff.csv")
		tol       = flag.Float64("tolerance", 0.0, "absolute |delta| under which a position counts as MATCH; defaults to exact match — pass -tolerance 0.01 for $0.01 slack")
	)
	flag.Parse()

	rb, err := engine.LoadRulebook(*rulesPath)
	if err != nil {
		fail("load rules: %v", err)
	}

	rows, positions, err := recon.LoadPositions(*posPath, *legsPath)
	if err != nil {
		fail("load positions: %v", err)
	}

	diffs := recon.Run(rb, rows, positions, recon.Options{Tolerance: *tol})

	if err := recon.WriteDiff(*outPath, diffs); err != nil {
		fail("write diff: %v", err)
	}

	fmt.Print(recon.FormatSummary(recon.Summarize(diffs)))
	fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "recon: "+format+"\n", args...)
	os.Exit(1)
}
