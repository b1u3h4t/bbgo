package server

import (
	"context"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/c9s/bbgo/pkg/exchange/binance"
	"github.com/c9s/bbgo/pkg/exchange/binance/binanceapi"
)

type bracketTierView struct {
	Bracket          int     `json:"bracket"`
	NotionalFloor    float64 `json:"notionalFloor"`
	NotionalCap      float64 `json:"notionalCap"`
	MaintMarginRatio float64 `json:"maintMarginRatio"`
	Cum              float64 `json:"cum"`
	MaxLeverage      int     `json:"maxLeverage"`
}

type bracketPositionRow struct {
	Symbol          string            `json:"symbol"`
	Side            string            `json:"side"`
	Notional        float64           `json:"notional"`
	Leverage        float64           `json:"leverage"`
	UnrealizedPnL   float64           `json:"unrealizedPnL"`
	Tier1Cap        float64           `json:"tier1Cap"` // 第1档名义上限（cum=0 档）
	CurrentBracket  int               `json:"currentBracket"`
	CurrentCap      float64           `json:"currentCap"`
	CurrentFloor    float64           `json:"currentFloor"`
	MMR             float64           `json:"mmr"`
	MMRPct          float64           `json:"mmrPct"`
	Cum             float64           `json:"cum"`
	MaintMargin     float64           `json:"maintMargin"`
	InCum0Tier      bool              `json:"inCum0Tier"`
	OverTier1By     float64           `json:"overTier1By"` // 名义超出第1档的部分；0=仍在第1档
	MaxLeverageTier int               `json:"maxLeverageTier"`
	Tiers           []bracketTierView `json:"tiers,omitempty"`
}

// pickLeverageBracket selects the highest matching tier for notional
// (notionalFloor ≤ N ≤ notionalCap). At shared boundaries the higher tier wins
// so MM stays continuous via cum.
func pickLeverageBracket(tiers []binanceapi.FuturesLeverageBracketTier, notional float64) (binanceapi.FuturesLeverageBracketTier, bool) {
	var best binanceapi.FuturesLeverageBracketTier
	found := false
	for _, t := range tiers {
		if notional >= t.NotionalFloor && notional <= t.NotionalCap {
			if !found || t.Bracket > best.Bracket {
				best = t
				found = true
			}
		}
	}
	if found {
		return best, true
	}
	// Fallback: largest floor still ≤ notional
	for _, t := range tiers {
		if notional >= t.NotionalFloor {
			if !found || t.Bracket > best.Bracket {
				best = t
				found = true
			}
		}
	}
	return best, found
}

func calcMaintMargin(notional, mmr, cum float64) float64 {
	mm := notional*mmr - cum
	if mm < 0 {
		return 0
	}
	return mm
}

func buildBracketPositionRows(
	positions []marginPositionRow,
	bracketsBySym map[string][]binanceapi.FuturesLeverageBracketTier,
) []bracketPositionRow {
	rows := make([]bracketPositionRow, 0, len(positions))
	for _, p := range positions {
		tiers := bracketsBySym[p.Symbol]
		if len(tiers) == 0 {
			continue
		}
		sort.Slice(tiers, func(i, j int) bool { return tiers[i].Bracket < tiers[j].Bracket })
		tier1 := tiers[0]
		cur, ok := pickLeverageBracket(tiers, p.Notional)
		if !ok {
			cur = tier1
		}
		mm := calcMaintMargin(p.Notional, cur.MaintMarginRatio, cur.Cum)
		over := 0.0
		if p.Notional > tier1.NotionalCap {
			over = p.Notional - tier1.NotionalCap
		}
		tierViews := make([]bracketTierView, 0, len(tiers))
		for _, t := range tiers {
			tierViews = append(tierViews, bracketTierView{
				Bracket:          t.Bracket,
				NotionalFloor:    t.NotionalFloor,
				NotionalCap:      t.NotionalCap,
				MaintMarginRatio: t.MaintMarginRatio,
				Cum:              t.Cum,
				MaxLeverage:      t.InitialLeverage,
			})
		}
		rows = append(rows, bracketPositionRow{
			Symbol:          p.Symbol,
			Side:            p.Side,
			Notional:        roundFloat(p.Notional, 2),
			Leverage:        p.Leverage,
			UnrealizedPnL:   p.UnrealizedPnL,
			Tier1Cap:        roundFloat(tier1.NotionalCap, 2),
			CurrentBracket:  cur.Bracket,
			CurrentCap:      roundFloat(cur.NotionalCap, 2),
			CurrentFloor:    roundFloat(cur.NotionalFloor, 2),
			MMR:             cur.MaintMarginRatio,
			MMRPct:          roundFloat(cur.MaintMarginRatio*100, 4),
			Cum:             roundFloat(cur.Cum, 4),
			MaintMargin:     roundFloat(mm, 2),
			InCum0Tier:      cur.Bracket == tier1.Bracket && tier1.Cum == 0,
			OverTier1By:     roundFloat(over, 2),
			MaxLeverageTier: cur.InitialLeverage,
			Tiers:           tierViews,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Notional > rows[j].Notional
	})
	return rows
}

func (s *Server) analysisBrackets(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ex, ok := session.Exchange.(*binance.Exchange)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leverage brackets require binance futures session"})
		return
	}
	ctx := c.Request.Context()
	positions := collectPositions(ctx, session)

	client := ex.GetFuturesClient()
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "futures client not ready"})
		return
	}

	// One call without symbol returns all brackets (fewer round-trips).
	all, err := client.NewFuturesGetLeverageBracketRequest().Do(ctx)
	if err != nil {
		// Fallback: per-symbol (some accounts / modes)
		all, err = fetchBracketsPerSymbol(ctx, client, positions)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "leverageBracket: " + err.Error()})
			return
		}
	}

	bySym := make(map[string][]binanceapi.FuturesLeverageBracketTier, len(all))
	for _, item := range all {
		bySym[item.Symbol] = item.Brackets
	}

	rows := buildBracketPositionRows(positions, bySym)
	sumN, sumMM := 0.0, 0.0
	inCum0, overTier1 := 0, 0
	for _, r := range rows {
		sumN += r.Notional
		sumMM += r.MaintMargin
		if r.InCum0Tier {
			inCum0++
		} else {
			overTier1++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session.Name,
		"formula": "MM = notional × MMR − cum",
		"note":    "第1档上限=cum=0 档的名义上限；跨档后 MMR 升高，cum 用于保持 MM 连续",
		"rules": gin.H{
			"title":   "MMR / cum 定位规则（币安 USDT-M）",
			"formula": "维持保证金 MM = 名义价值 Notional × 维持保证金率 MMR − 维持保证金速算额 cum",
			"items": []string{
				"名义价值 Notional = |仓位数量| × 标记价格（按币种分别算档位，再汇总账户 MM）。",
				"档位表来自 GET /fapi/v1/leverageBracket：每档有 notionalFloor～notionalCap、MMR（maintMarginRatio）、cum、该档最高杠杆。",
				"定位当前档：取满足 notionalFloor ≤ Notional ≤ notionalCap 的档位；若边界同时落在两档，取更高档（靠 cum 保持 MM 连续）。",
				"MMR：该档维持保证金率，加在名义上，与你选的杠杆无关；仓越大通常 MMR 越高。",
				"cum（Maintenance Amount）：档位速算扣减额。第1档多为 0；更高档 cum>0，使跨档时 MM 不突变。例：名义刚到第2档下限 N 时，N×MMR₂−cum₂ ≈ 上一档顶的 MM。",
				"cum=0 档：通常即第1档（最低 MMR）。「第1档上限」= 该档 notionalCap；名义超过后越档，MMR/cum 按新档计算。",
				"强平看账户保证金余额是否低于各币 MM 之和（再加其他调整），不是看 IM，也不是「权益只剩 MMR%」。",
			},
			"source": "https://www.binance.com/en/futures/trading-rules/perpetual/leverage-margin",
		},
		"summary": gin.H{
			"positionCount":  len(rows),
			"sumNotional":    roundFloat(sumN, 2),
			"sumMaintMargin": roundFloat(sumMM, 2),
			"inCum0Tier":     inCum0,
			"overTier1":      overTier1,
		},
		"positions": rows,
	})
}

func fetchBracketsPerSymbol(
	ctx context.Context,
	client *binanceapi.FuturesRestClient,
	positions []marginPositionRow,
) ([]binanceapi.FuturesLeverageBracketSymbol, error) {
	out := make([]binanceapi.FuturesLeverageBracketSymbol, 0, len(positions))
	seen := map[string]struct{}{}
	for _, p := range positions {
		if _, ok := seen[p.Symbol]; ok {
			continue
		}
		seen[p.Symbol] = struct{}{}
		rows, err := client.NewFuturesGetLeverageBracketRequest().Symbol(p.Symbol).Do(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}
