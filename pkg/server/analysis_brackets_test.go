package server

import (
	"testing"

	"github.com/c9s/bbgo/pkg/exchange/binance/binanceapi"
)

func TestPickLeverageBracket(t *testing.T) {
	tiers := []binanceapi.FuturesLeverageBracketTier{
		{Bracket: 1, NotionalFloor: 0, NotionalCap: 20000, MaintMarginRatio: 0.01, Cum: 0},
		{Bracket: 2, NotionalFloor: 20000, NotionalCap: 200000, MaintMarginRatio: 0.015, Cum: 100},
		{Bracket: 3, NotionalFloor: 200000, NotionalCap: 1000000, MaintMarginRatio: 0.02, Cum: 1100},
	}

	tests := []struct {
		n    float64
		want int
	}{
		{10000, 1},
		{20000, 2}, // boundary → higher tier (MM continuous)
		{26863, 2},
		{250000, 3},
	}
	for _, tt := range tests {
		got, ok := pickLeverageBracket(tiers, tt.n)
		if !ok {
			t.Fatalf("notional %v: not found", tt.n)
		}
		if got.Bracket != tt.want {
			t.Fatalf("notional %v: bracket=%d want %d", tt.n, got.Bracket, tt.want)
		}
	}
}

func TestCalcMaintMargin(t *testing.T) {
	// NEAR-like: 26863 * 1.5% - 100
	mm := calcMaintMargin(26863, 0.015, 100)
	want := 26863*0.015 - 100
	if mm != want {
		t.Fatalf("mm=%v want %v", mm, want)
	}
	if calcMaintMargin(100, 0.01, 50) != 0 {
		t.Fatalf("expected floor at 0")
	}
}

func TestBuildBracketPositionRows(t *testing.T) {
	positions := []marginPositionRow{
		{Symbol: "NEARUSDT", Side: "SHORT", Notional: 26863.31, Leverage: 3, UnrealizedPnL: -100},
		{Symbol: "SUIUSDT", Side: "LONG", Notional: 9675.92, Leverage: 3, UnrealizedPnL: -10},
	}
	bySym := map[string][]binanceapi.FuturesLeverageBracketTier{
		"NEARUSDT": {
			{Bracket: 1, NotionalFloor: 0, NotionalCap: 20000, MaintMarginRatio: 0.01, Cum: 0, InitialLeverage: 75},
			{Bracket: 2, NotionalFloor: 20000, NotionalCap: 200000, MaintMarginRatio: 0.015, Cum: 100, InitialLeverage: 50},
		},
		"SUIUSDT": {
			{Bracket: 1, NotionalFloor: 0, NotionalCap: 20000, MaintMarginRatio: 0.01, Cum: 0, InitialLeverage: 75},
		},
	}
	rows := buildBracketPositionRows(positions, bySym)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].Symbol != "NEARUSDT" || rows[0].InCum0Tier || rows[0].CurrentBracket != 2 {
		t.Fatalf("NEAR row unexpected: %+v", rows[0])
	}
	if rows[0].Tier1Cap != 20000 || rows[0].OverTier1By < 6800 {
		t.Fatalf("NEAR tier1 fields: %+v", rows[0])
	}
	if !rows[1].InCum0Tier || rows[1].CurrentBracket != 1 || rows[1].Cum != 0 {
		t.Fatalf("SUI row unexpected: %+v", rows[1])
	}
}
