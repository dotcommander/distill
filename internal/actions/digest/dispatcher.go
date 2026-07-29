package digest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dotcommander/distill/internal/ai"
)

// RouteKind identifies whether a provider attempt used the configured role
// model or Distill's explicit cross-provider fallback model.
type RouteKind string

const (
	// RoutePrimary selects the model configured for the current digest role.
	RoutePrimary RouteKind = "primary"
	// RouteFallback selects the explicit cross-provider recovery route.
	RouteFallback RouteKind = "fallback"
)

// Route is provider-safe completion metadata. Provider and Model are persisted
// in observability sidecars; Completer is never serialized.
type Route struct {
	Completer Completer `json:"-"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Kind      RouteKind `json:"kind"`
	Available bool      `json:"available"`
}

// RoutePair supplies the primary and optional fallback route for a digest
// role. Explicitly pinned models simply leave Fallback unavailable.
type RoutePair struct {
	Primary  Route
	Fallback Route
}

// RoleRoutes maps stable digest stage names (research, outline, section,
// edit, fuse, judge) to their provider routes.
type RoleRoutes map[string]RoutePair

// Dispatcher runs all explicit digest recovery. It holds a distinct budget
// reservation for each planned attempt until that attempt's client acquires it
// immediately before transport, preventing optional recovery from starving a
// concurrently starting mandatory first attempt.
type Dispatcher struct {
	Routes           RoleRoutes
	Budget           *ai.RequestBudget
	Plan             *CallPlan
	PrimaryAttempts  int
	RetryDelay       time.Duration
	mu               sync.Mutex
	mandatoryPending int
	observer         func(AttemptEvent)
	providerUsage    map[string]int
}

// AttemptEvent is the sanitized transport-level record emitted for each
// started primary or fallback request.
type AttemptEvent struct {
	Role     string
	Route    Route
	Attempt  int
	Sent     bool
	Started  time.Time
	Duration time.Duration
	Err      error
}

// NewDispatcher initializes runtime mandatory reservations from plan.
func NewDispatcher(routes RoleRoutes, budget *ai.RequestBudget, plan *CallPlan, primaryAttempts int, delay time.Duration) *Dispatcher {
	if primaryAttempts < 1 {
		primaryAttempts = 1
	}
	if delay <= 0 {
		delay = time.Second
	}
	pending := 0
	if plan != nil {
		pending = plan.RemainingMandatory()
	}
	return &Dispatcher{Routes: routes, Budget: budget, Plan: plan, PrimaryAttempts: primaryAttempts, RetryDelay: delay, mandatoryPending: pending, providerUsage: make(map[string]int)}
}

// SetObserver installs a sanitized attempt observer for runtime reporting.
func (d *Dispatcher) SetObserver(observer func(AttemptEvent)) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.observer = observer
	d.mu.Unlock()
}

//nolint:revive // A request attempt has six inseparable transport inputs.
func (d *Dispatcher) invoke(ctx context.Context, reservation *ai.RequestReservation, role string, route Route, attempt int, prompt string, timeout time.Duration) (string, error) {
	started := time.Now()
	if reservation != nil {
		defer reservation.Release()
		ctx = ai.ContextWithRequestReservation(ctx, reservation)
	}
	out, err := complete(ctx, route.Completer, prompt, timeout)
	sent := reservation != nil && reservation.Sent()
	event := AttemptEvent{Role: role, Route: route, Attempt: attempt, Sent: sent, Started: started, Duration: time.Since(started), Err: err}
	d.mu.Lock()
	observer := d.observer
	if sent {
		d.providerUsage[route.Provider+"/"+route.Model+"/"+string(route.Kind)]++
	}
	d.mu.Unlock()
	if observer != nil {
		observer(event)
	}
	return out, err
}

// ProviderUsage returns a snapshot of sent attempts grouped by provider, model,
// and primary/fallback route kind.
func (d *Dispatcher) ProviderUsage() map[string]int {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]int, len(d.providerUsage))
	for key, count := range d.providerUsage {
		out[key] = count
	}
	return out
}

// Complete dispatches one logical stage call. A valid primary attempt is used
// first; retryable failures get configured primary retries, then exactly one
// fallback attempt when a fallback route is available.
//
//nolint:revive // The route and attempt are required for durable provenance.
func (d *Dispatcher) Complete(ctx context.Context, role, prompt string, timeout time.Duration) (string, Route, int, error) {
	return d.CompleteValidated(ctx, role, prompt, timeout, nil)
}

// CompleteValidated is Complete with response validation inside the recovery
// loop. Invalid output is retryable and may reach the explicit fallback route.
//
//nolint:gocognit,gocyclo,revive // The recovery state machine keeps reservations and retries together.
func (d *Dispatcher) CompleteValidated(ctx context.Context, role, prompt string, timeout time.Duration, validate func(string) error) (string, Route, int, error) {
	pair, ok := d.Routes[role]
	if !ok || pair.Primary.Completer == nil {
		return "", Route{}, 0, fmt.Errorf("digest: no primary route for %s", role)
	}
	primaryReservation, err := d.reserveMandatory(ctx)
	if err != nil {
		return "", pair.Primary, 0, err
	}
	var lastErr error
	var fallbackReservation *ai.RequestReservation
	fallbackAllowed := false
	reservation := primaryReservation
	for attempt := 1; attempt <= d.PrimaryAttempts; attempt++ {
		out, err := d.invoke(ctx, reservation, role, pair.Primary, attempt, prompt, timeout)
		if err == nil && out != "" && validate != nil {
			if validationErr := validate(out); validationErr != nil {
				err = fmt.Errorf("%w: invalid response: %w", ai.ErrEmptyResponse, validationErr)
			}
		}
		if err == nil && out != "" {
			fallbackReservation.Release()
			return out, pair.Primary, attempt, nil
		}
		if err == nil {
			err = ai.ErrEmptyResponse
		}
		lastErr = err
		if !ai.IsRetryable(err) || ctx.Err() != nil {
			break
		}
		// Reserve fallback before primary retry so a run at the hard ceiling
		// retains one cross-provider escape hatch.
		if !fallbackAllowed && pair.Fallback.Available && pair.Fallback.Completer != nil {
			fallbackReservation, fallbackAllowed = d.reserveOptional(ctx)
		}
		if attempt == d.PrimaryAttempts {
			break
		}
		var retryAllowed bool
		reservation, retryAllowed = d.reserveOptional(ctx)
		if !retryAllowed {
			break
		}
		timer := time.NewTimer(retryBackoff(attempt, d.RetryDelay))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			reservation.Release()
			fallbackReservation.Release()
			return "", Route{}, attempt, ctx.Err()
		case <-timer.C:
		}
	}
	//nolint:nestif // Fallback must atomically consume its reservation and validate its response.
	if fallbackAllowed {
		out, err := d.invoke(ctx, fallbackReservation, role, pair.Fallback, 1, prompt, timeout)
		if err == nil && out != "" && validate != nil {
			if validationErr := validate(out); validationErr != nil {
				err = fmt.Errorf("%w: invalid response: %w", ai.ErrEmptyResponse, validationErr)
			}
		}
		if err == nil && out != "" {
			return out, pair.Fallback, 1, nil
		}
		if err == nil {
			err = ai.ErrEmptyResponse
		}
		return "", pair.Fallback, 1, fmt.Errorf("digest: %s fallback: %w", role, err)
	}
	return "", pair.Primary, d.PrimaryAttempts, lastErr
}

func (d *Dispatcher) reserveMandatory(ctx context.Context) (*ai.RequestReservation, error) {
	if d == nil {
		return nil, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Budget == nil {
		if d.mandatoryPending > 0 {
			d.mandatoryPending--
		}
		return nil, nil
	}
	reservation, err := d.Budget.ReserveContext(ctx, "text")
	if d.mandatoryPending > 0 {
		d.mandatoryPending--
	}
	return reservation, err
}

// ReleaseMandatory removes verified first-attempt reservations satisfied by a
// response sidecar or an exact cache hit.
func (d *Dispatcher) ReleaseMandatory(n int) {
	if d == nil || n < 1 {
		return
	}
	d.mu.Lock()
	d.mandatoryPending = max(0, d.mandatoryPending-n)
	d.mu.Unlock()
}

// reserveOptional holds capacity for one future retry or fallback only when it
// can coexist with every mandatory first attempt that has not yet started.
func (d *Dispatcher) reserveOptional(ctx context.Context) (*ai.RequestReservation, bool) {
	if d == nil || d.Budget == nil {
		return nil, true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if limit := d.Budget.Limit(); limit > 0 && d.Budget.Used()+d.Budget.Reserved()+d.mandatoryPending+1 > limit {
		return nil, false
	}
	reservation, err := d.Budget.ReserveContext(ctx, "text")
	if err != nil {
		return nil, false
	}
	return reservation, true
}

// RemainingMandatory reports the runtime first-attempt reservation remaining.
func (d *Dispatcher) RemainingMandatory() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.mandatoryPending
}

// IsBudgetError is available to callers that need to distinguish unsent calls
// from provider failures without inspecting provider strings.
func IsBudgetError(err error) bool { return errors.Is(err, ai.ErrRequestBudgetExhausted) }
