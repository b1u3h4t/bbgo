package prospec

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/stretchr/testify/require"
)

// Holding + majors pool for live comparison (analysis only).
var liveBTSymbols = []string{
	"BTCUSDT", "ETHUSDT",
	"NEARUSDT", "BNBUSDT", "AVAXUSDT", "LINKUSDT", "SOLUSDT",
	"ENAUSDT", "XRPUSDT", "HYPEUSDT", "ZECUSDT",
}

func fetchBinanceFuturesKlines(symbol, interval string, limit int) ([]types.KLine, error) {
	url := fmt.Sprintf(
		"https://fapi.binance.com/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		symbol, interval, limit,
	)
	client := &http.Client{Timeout: 25 * time.Second}
	if p := os.Getenv("https_proxy"); p == "" {
		if p = os.Getenv("HTTPS_PROXY"); p == "" {
			_ = p
		}
	}
	// Prefer explicit env; http.Client uses DefaultTransport which honors HTTPS_PROXY.
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var raw [][]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]types.KLine, 0, len(raw))
	iv := types.Interval(interval)
	for _, row := range raw {
		if len(row) < 6 {
			continue
		}
		openT := int64(row[0].(float64))
		o, _ := row[1].(string)
		h, _ := row[2].(string)
		l, _ := row[3].(string)
		c, _ := row[4].(string)
		of, _ := fixedpoint.NewFromString(o)
		hf, _ := fixedpoint.NewFromString(h)
		lf, _ := fixedpoint.NewFromString(l)
		cf, _ := fixedpoint.NewFromString(c)
		out = append(out, types.KLine{
			Symbol: symbol, Interval: iv,
			StartTime: types.Time(time.UnixMilli(openT).UTC()),
			Open: of, High: hf, Low: lf, Close: cf,
			Closed: true,
		})
	}
	return out, nil
}

func TestLiveTrendlineMethodCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	if os.Getenv("PROSPEC_LIVE_BT") == "0" {
		t.Skip("disabled")
	}
	type row struct {
		Symbol string
		IV     string
		Rows   []MethodBTStats
	}
	var report []row
	for _, sy := range liveBTSymbols {
		for _, iv := range []string{"4h", "1d"} {
			limit, horizon := 300, 24
			if iv == "1d" {
				limit, horizon = 260, 12
			}
			ks, err := fetchBinanceFuturesKlines(sy, iv, limit)
			if err != nil {
				t.Logf("%s %s fetch: %v", sy, iv, err)
				continue
			}
			require.Greater(t, len(ks), 80)
			cmp := Compare123Methods(ks, 3, horizon)
			report = append(report, row{Symbol: sy, IV: iv, Rows: cmp})
			t.Logf("%s %s → hl trades=%d wr=%.1f avgR=%.3f | ols %d/%.1f/%.3f | ransac %d/%.1f/%.3f",
				sy, iv,
				cmp[0].Trades123, cmp[0].WinRate123, cmp[0].AvgR123,
				cmp[1].Trades123, cmp[1].WinRate123, cmp[1].AvgR123,
				cmp[2].Trades123, cmp[2].WinRate123, cmp[2].AvgR123,
			)
			time.Sleep(120 * time.Millisecond)
		}
	}
	// aggregate
	agg := map[string]*MethodBTStats{}
	for _, m := range []string{"hl", "ols", "ransac"} {
		agg[m] = &MethodBTStats{Method: m}
	}
	for _, r := range report {
		if r.IV != "4h" {
			continue
		}
		for _, st := range r.Rows {
			a := agg[st.Method]
			a.Trades123 += st.Trades123
			a.Wins123 += st.Wins123
			a.Losses123 += st.Losses123
			a.AvgR123 += st.AvgR123 * float64(st.Trades123)
		}
	}
	t.Log("=== 4h AGGREGATE (holdings+BTC/ETH) ===")
	for _, m := range []string{"hl", "ols", "ransac"} {
		a := agg[m]
		if a.Trades123 > 0 {
			a.WinRate123 = round8(100 * float64(a.Wins123) / float64(a.Trades123))
			a.AvgR123 = round8(a.AvgR123 / float64(a.Trades123))
		}
		t.Logf("%s: trades=%d winRate=%.2f%% avgR=%.4f", m, a.Trades123, a.WinRate123, a.AvgR123)
	}
}
