package bracket

import (
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// JobState is the lifecycle state of a bracket job.
type JobState string

const (
	StatePendingEntry JobState = "pending_entry"
	StatePendingExit  JobState = "pending_exit"
	StateDone         JobState = "done"
	StateCanceled     JobState = "canceled"
)

// Job tracks one entry order and its optional TP/SL exits.
type Job struct {
	ID         string           `json:"id"`
	State      JobState         `json:"state"`
	Symbol     string           `json:"symbol"`
	Side       types.SideType   `json:"side"`
	Quantity   fixedpoint.Value `json:"quantity"`
	EntryPrice fixedpoint.Value `json:"entryPrice,omitempty"`
	TpPrice    fixedpoint.Value `json:"tpPrice"`
	SlPrice    fixedpoint.Value `json:"slPrice"` // <= 0 means no stop-loss

	EntryOrderID uint64 `json:"entryOrderID,omitempty"`
	TpOrderID    uint64 `json:"tpOrderID,omitempty"`
	SlOrderID    uint64 `json:"slOrderID,omitempty"`

	ExecutedQuantity fixedpoint.Value `json:"executedQuantity,omitempty"`
	LastError        string           `json:"lastError,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// WantSL reports whether a stop-loss exit should be placed.
func (j *Job) WantSL() bool {
	return j.SlPrice.Sign() > 0
}

// ExitSide is the reduce-only side opposite to the entry.
func (j *Job) ExitSide() types.SideType {
	return j.Side.Reverse()
}

// Active returns true if the job still needs stream handling.
func (j *Job) Active() bool {
	return j.State == StatePendingEntry || j.State == StatePendingExit
}
