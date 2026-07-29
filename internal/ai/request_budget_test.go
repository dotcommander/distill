package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestNewRequestBudgetRejectsNegativeLimit(t *testing.T) {
	if _, err := NewRequestBudget(-1); err == nil {
		t.Fatal("NewRequestBudget(-1) succeeded")
	}
}

func TestRequestBudgetCanceledBeforeAcquireIsNotCounted(t *testing.T) {
	budget, err := NewRequestBudget(1)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := budget.AcquireContext(ctx, "text"); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire error = %v, want context canceled", err)
	}
	if got := budget.Used(); got != 0 {
		t.Fatalf("used = %d, want 0", got)
	}
}

func TestRequestReservationHoldsCapacityUntilItsOwnAcquire(t *testing.T) {
	budget, err := NewRequestBudget(1)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	reservation, err := budget.ReserveContext(context.Background(), "text")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if got := budget.Reserved(); got != 1 {
		t.Fatalf("reserved = %d, want 1", got)
	}
	if err := budget.Acquire("text"); !errors.Is(err, ErrRequestBudgetExhausted) {
		t.Fatalf("unreserved acquire = %v, want exhaustion", err)
	}
	ctx := ContextWithRequestReservation(context.Background(), reservation)
	if err := budget.AcquireContext(ctx, "text"); err != nil {
		t.Fatalf("reserved acquire: %v", err)
	}
	if !reservation.Sent() || budget.Used() != 1 || budget.Reserved() != 0 {
		t.Fatalf("sent=%t used=%d reserved=%d, want true/1/0", reservation.Sent(), budget.Used(), budget.Reserved())
	}
}

func TestCanceledRequestReservationIsReleasedWithoutCounting(t *testing.T) {
	budget, err := NewRequestBudget(1)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	reservation, err := budget.ReserveContext(context.Background(), "text")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	ctx, cancel := context.WithCancel(ContextWithRequestReservation(context.Background(), reservation))
	cancel()
	if err := budget.AcquireContext(ctx, "text"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reserved acquire = %v, want context canceled", err)
	}
	if reservation.Sent() || budget.Used() != 0 || budget.Reserved() != 0 {
		t.Fatalf("sent=%t used=%d reserved=%d, want false/0/0", reservation.Sent(), budget.Used(), budget.Reserved())
	}
}

func TestRequestBudgetUnlimitedCountsReservations(t *testing.T) {
	budget, err := NewRequestBudget(0)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	for range 3 {
		if err := budget.Acquire("text"); err != nil {
			t.Fatalf("acquire: %v", err)
		}
	}
	if got := budget.Used(); got != 3 {
		t.Fatalf("used = %d, want 3", got)
	}
}

func TestRequestBudgetConcurrentContentionNeverExceedsLimit(t *testing.T) {
	const limit = 7
	budget, err := NewRequestBudget(limit)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if err := budget.Acquire("text"); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			} else if !errors.Is(err, ErrRequestBudgetExhausted) {
				t.Errorf("acquire error = %v", err)
			}
		})
	}
	wg.Wait()
	if successes != limit || budget.Used() != limit {
		t.Fatalf("successes=%d used=%d, want %d", successes, budget.Used(), limit)
	}
}
