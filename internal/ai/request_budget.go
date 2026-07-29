package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrRequestBudgetExhausted marks an attempt that was not sent because the
// run-wide provider request limit has already been reached.
var ErrRequestBudgetExhausted = errors.New("provider request budget exhausted")

// RequestBudget is the run-wide provider request ledger. A zero limit is
// unlimited, but still counts successful reservations so summaries can state
// whether a cache-only run made any provider requests.
type RequestBudget struct {
	limit int64

	mu       sync.Mutex
	used     int64
	reserved int64
}

// RequestReservation holds capacity for one specific future provider request.
// A reservation does not increment Used until its owner acquires it immediately
// before transport. Release is idempotent and returns an unacquired permit to
// the budget.
type RequestReservation struct {
	budget   *RequestBudget
	acquired bool
	released bool
}

type reservationContextKey struct{}

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
	return b.AcquireContext(context.Background(), kind)
}

// AcquireContext reserves one outbound request after first observing caller
// cancellation. A request canceled before transport invocation is therefore
// not counted; once this method succeeds, the attempt is counted even if it
// later fails or is canceled in flight.
func (b *RequestBudget) AcquireContext(ctx context.Context, kind string) error {
	if b == nil {
		return nil
	}
	if reservation, ok := ctx.Value(reservationContextKey{}).(*RequestReservation); ok && reservation != nil {
		if reservation.budget != b {
			return errors.New("request reservation belongs to a different budget")
		}
		return reservation.AcquireContext(ctx, kind)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit == 0 {
		b.used++
		return nil
	}
	if b.used+b.reserved >= b.limit {
		return fmt.Errorf("%w before %s request (%d/%d)", ErrRequestBudgetExhausted, kind, b.used, b.limit)
	}
	b.used++
	return nil
}

// ReserveContext holds one slot for a particular future provider request. It
// is used by the digest dispatcher so concurrent optional recovery attempts
// cannot consume capacity promised to mandatory first attempts.
func (b *RequestBudget) ReserveContext(ctx context.Context, kind string) (*RequestReservation, error) {
	if b == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 && b.used+b.reserved >= b.limit {
		return nil, fmt.Errorf("%w before %s request (%d/%d)", ErrRequestBudgetExhausted, kind, b.used, b.limit)
	}
	b.reserved++
	return &RequestReservation{budget: b}, nil
}

// ContextWithRequestReservation carries reservation ownership to the client
// that performs the eventual just-before-transport budget acquisition.
func ContextWithRequestReservation(ctx context.Context, reservation *RequestReservation) context.Context {
	if reservation == nil {
		return ctx
	}
	return context.WithValue(ctx, reservationContextKey{}, reservation)
}

// AcquireContext turns this reservation into one counted provider request. It
// returns the caller context error without counting a canceled-before-send
// attempt, and cannot be acquired more than once.
func (r *RequestReservation) AcquireContext(ctx context.Context, kind string) error {
	if r == nil || r.budget == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		r.Release()
		return err
	}
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return fmt.Errorf("request reservation released before %s request", kind)
	}
	if r.acquired {
		return fmt.Errorf("request reservation already acquired for %s request", kind)
	}
	if b.reserved < 1 {
		return fmt.Errorf("request reservation missing before %s request", kind)
	}
	b.reserved--
	b.used++
	r.acquired = true
	return nil
}

// Release returns an unacquired reservation to the budget. A sent reservation
// remains counted, so release after transport completion is harmless.
func (r *RequestReservation) Release() {
	if r == nil || r.budget == nil {
		return
	}
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	if !r.acquired && b.reserved > 0 {
		b.reserved--
	}
}

// Sent reports whether this exact attempt acquired its budget slot.
func (r *RequestReservation) Sent() bool {
	if r == nil || r.budget == nil {
		return false
	}
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	return r.acquired
}

// Used returns the number of reservations, including failed outbound attempts.
func (b *RequestBudget) Used() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.used)
}

// Reserved reports capacity held for future specific attempts but not yet
// counted as provider traffic. It is primarily useful to dispatchers and
// deterministic concurrency tests.
func (b *RequestBudget) Reserved() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.reserved)
}

// Limit returns the configured limit; zero means unlimited.
func (b *RequestBudget) Limit() int {
	if b == nil {
		return 0
	}
	return int(b.limit)
}
