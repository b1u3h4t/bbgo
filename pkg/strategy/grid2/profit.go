package grid2

import (
	"fmt"
	"strconv"
	"time"

	"github.com/slack-go/slack"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/style"
	"github.com/c9s/bbgo/pkg/types"
)

// GridProfit is emitted when a grid round-trip completes (closing fill + reverse order placed).
//
// For USDT-M futures:
//   - Profit / RealizedProfit: position avg-cost PnL (aligned with Binance realizedPnl)
//   - TwinPinProfit: (sellPin-buyPin)*qty theoretical grid-level arb — for tuning spread/qty
//   - Cumulative*: running totals after this round
type GridProfit struct {
	Symbol   string           `json:"symbol,omitempty"`
	Currency string           `json:"currency"`
	Profit   fixedpoint.Value `json:"profit"`
	Time     time.Time        `json:"time"`
	Order    types.Order      `json:"order"`

	// TwinPinProfit is the theoretical one-level grid spread profit (for parameter tuning).
	TwinPinProfit fixedpoint.Value `json:"twinPinProfit,omitempty"`

	// RealizedProfit mirrors Profit for USDT-M (explicit name in Slack/logs).
	RealizedProfit fixedpoint.Value `json:"realizedProfit,omitempty"`

	// CumulativeRealized is TotalQuoteProfit (or base) after adding this round.
	CumulativeRealized fixedpoint.Value `json:"cumulativeRealized,omitempty"`

	// CumulativeTwinPin is TotalTwinPinProfit after adding this round.
	CumulativeTwinPin fixedpoint.Value `json:"cumulativeTwinPin,omitempty"`

	// ArbitrageCount is the number of completed rounds including this one.
	ArbitrageCount int `json:"arbitrageCount,omitempty"`
}

func (p *GridProfit) String() string {
	msg := fmt.Sprintf("GRID PROFIT: realized=%f twinPin=%f %s @ %s orderID %d cumulative=%f rounds=%d",
		p.Profit.Float64(), p.TwinPinProfit.Float64(), p.Currency, p.Time.String(), p.Order.OrderID,
		p.CumulativeRealized.Float64(), p.ArbitrageCount)
	return msg
}

func (p *GridProfit) PlainText() string {
	sym := p.Symbol
	if sym == "" {
		sym = p.Order.Symbol
	}
	return fmt.Sprintf("%s round: realized %s %s | twin-pin %s %s | cumulative %s %s | rounds %d | order %d",
		sym,
		style.PnLSignString(p.Profit), p.Currency,
		style.PnLSignString(p.TwinPinProfit), p.Currency,
		style.PnLSignString(p.CumulativeRealized), p.Currency,
		p.ArbitrageCount,
		p.Order.OrderID,
	)
}

func (p *GridProfit) SlackAttachment() slack.Attachment {
	sym := p.Symbol
	if sym == "" {
		sym = p.Order.Symbol
	}
	title := fmt.Sprintf("%s Grid Round %s %s", sym, style.PnLSignString(p.Profit), p.Currency)
	color := style.PnLColor(p.Profit)
	fields := []slack.AttachmentField{
		{
			Title: "本笔已实现 (对齐币安 realizedPnl)",
			Value: fmt.Sprintf("%s %s", style.PnLSignString(p.Profit), p.Currency),
			Short: true,
		},
		{
			Title: "本笔档距利润 (调优用)",
			Value: fmt.Sprintf("%s %s", style.PnLSignString(p.TwinPinProfit), p.Currency),
			Short: true,
		},
		{
			Title: "累计已实现",
			Value: fmt.Sprintf("%s %s", style.PnLSignString(p.CumulativeRealized), p.Currency),
			Short: true,
		},
		{
			Title: "累计档距利润",
			Value: fmt.Sprintf("%s %s", style.PnLSignString(p.CumulativeTwinPin), p.Currency),
			Short: true,
		},
		{
			Title: "闭环次数",
			Value: strconv.Itoa(p.ArbitrageCount),
			Short: true,
		},
		{
			Title: "OrderID",
			Value: strconv.FormatUint(p.Order.OrderID, 10),
			Short: true,
		},
		{
			Title: "Time",
			Value: p.Time.UTC().Format(time.RFC3339),
			Short: true,
		},
	}
	return slack.Attachment{
		Title:  title,
		Color:  color,
		Text:   "每笔闭环同时给出：已实现盈亏（跟踪实盘）+ 档距理论利润（调参）+ 累计。",
		Fields: fields,
	}
}
