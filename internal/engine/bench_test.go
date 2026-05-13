package engine

import "testing"

// BenchmarkBindSlots_Direct measures bindSlots in isolation (no CEL eval).
func BenchmarkBindSlots_Direct_Box(b *testing.B) {
	slots := []LegSlot{
		{Name: "bc", Side: "long", Kind: "option", OptionType: "call"},
		{Name: "bp", Side: "short", Kind: "option", OptionType: "put"},
		{Name: "sp", Side: "long", Kind: "option", OptionType: "put"},
		{Name: "sc", Side: "short", Kind: "option", OptionType: "call"},
	}
	legs := []Leg{
		{Side: Long, Kind: OptionKind, OptionType: "call", K: 95},
		{Side: Short, Kind: OptionKind, OptionType: "put", K: 95},
		{Side: Long, Kind: OptionKind, OptionType: "put", K: 105},
		{Side: Short, Kind: OptionKind, OptionType: "call", K: 105},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := bindSlots(legs, slots); !ok {
			b.Fatal("bind failed")
		}
	}
}

func BenchmarkBindSlots_Direct_Vertical(b *testing.B) {
	slots := []LegSlot{
		{Name: "long_leg", Side: "long", Kind: "option"},
		{Name: "short_leg", Side: "short", Kind: "option"},
	}
	legs := []Leg{
		{Side: Long, Kind: OptionKind, OptionType: "call", K: 100},
		{Side: Short, Kind: OptionKind, OptionType: "call", K: 105},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := bindSlots(legs, slots); !ok {
			b.Fatal("bind failed")
		}
	}
}

// BenchmarkBindSlots_Vertical exercises a 2-slot rule (one long, one short).
func BenchmarkBindSlots_Vertical(b *testing.B) {
	rb, err := LoadRulebook(rulesPath)
	if err != nil {
		b.Fatal(err)
	}
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 5, P0: 5, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed", Underlying: "X", Expiration: "2026-01-01"},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 105, P: 2, P0: 2, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed", Underlying: "X", Expiration: "2026-01-01"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rb.Evaluate(pos, MarginAccount, Initial); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBindSlots_Box exercises a 4-slot rule (long box).
func BenchmarkBindSlots_Box(b *testing.B) {
	rb, err := LoadRulebook(rulesPath)
	if err != nil {
		b.Fatal(err)
	}
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 95, P: 8, P0: 8, Qty: 1, Mult: 100,
				Style: "american", Expiration: "2026-01-01"},
			{Side: Short, Kind: OptionKind, OptionType: "put", K: 95, P: 1, P0: 1, Qty: 1, Mult: 100,
				Style: "american", Expiration: "2026-01-01"},
			{Side: Long, Kind: OptionKind, OptionType: "put", K: 105, P: 6, P0: 6, Qty: 1, Mult: 100,
				Style: "american", Expiration: "2026-01-01"},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 105, P: 3, P0: 3, Qty: 1, Mult: 100,
				Style: "american", Expiration: "2026-01-01"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rb.Evaluate(pos, MarginAccount, Initial); err != nil {
			b.Fatal(err)
		}
	}
}
