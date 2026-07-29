package ai

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// ErrRequestBudgetExhausted marks an attempt that was not sent because the
// run-wide provider request limit has already been reached.
var ErrRequestBudgetExhausted = errors.New("provider request budget exhausted")

// RequestBudget is the run-wide provider request ledger. A zero limit is
// unlimited, but still counts successful reservations so summaries can state
// whether a cache-only run made any provider requests.
type RequestBudget struct {
	limit int64
	used  atomic.Int64
}

// NewRequestBudget returns a budget with limit requests. A zero limit is
// unlimited; negative limits are invalid.
func NewRequestBudget(limit int) (*RequestBudget, error) {
	if limit < 0 {
		return nil, fmt.Errorf("provider request budget must be >= 0, got %d", limit)
	}
	return &RequestBudget{limit: int64(limit)}, nil
}

// Acquire reserves one provider request immediately before it is sent.
func (b *RequestBudget) Acquire(kind string) error {
	if b == nil {
		return nil
	}
	if b.limit == 0 {
		b.used.Add(1)
		return nil
	}
	for {
		used := b.used.Load()
		if used >= b.limit {
			return fmt.Errorf("%w before %s request (%d/%d)", ErrRequestBudgetExhausted, kind, used, b.limit)
		}
		if b.used.CompareAndSwap(used, used+1) {
			return nil
		}
	}
}

// Used returns the number of reservations, including failed outbound attempts.
func (b *RequestBudget) Used() int {
	if b == nil {
		return 0
	}
	return int(b.used.Load())
}

// Limit returns the configured limit; zero means unlimited.
func (b *RequestBudget) Limit() int {
	if b == nil {
		return 0
	}
	return int(b.limit)
}
