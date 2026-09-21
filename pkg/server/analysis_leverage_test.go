package server

import "testing"

func TestResolvePositionLeverage(t *testing.T) {
	tests := []struct {
		name   string
		lev    float64
		notion float64
		im     float64
		want   float64
	}{
		{name: "explicit", lev: 5, notion: 10000, im: 2000, want: 5},
		{name: "derive3x", lev: 0, notion: 22314, im: 7438, want: 3},
		{name: "derive4x", lev: 0, notion: 10651, im: 2701, want: 4},
		{name: "missing", lev: 0, notion: 0, im: 0, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePositionLeverage(tt.lev, tt.notion, tt.im)
			if got != tt.want {
				t.Fatalf("resolvePositionLeverage(%v,%v,%v)=%v want %v", tt.lev, tt.notion, tt.im, got, tt.want)
			}
		})
	}
}
