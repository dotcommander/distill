package ai

import (
	"errors"
	"sync"
	"testing"
)

func TestNewRequestBudgetRejectsNegativeLimit(t *testing.T) {
	if _, err := NewRequestBudget(-1); err == nil {
		t.Fatal("NewRequestBudget(-1) succeeded")
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
