package agent

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

// RetryPolicy provides bounded exponential backoff with symmetric jitter.
type RetryPolicy struct {
	Initial    time.Duration
	Maximum    time.Duration
	Multiplier float64
	Jitter     float64
	Rand       func() float64
	Sleep      func(context.Context, time.Duration) error
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.Initial <= 0 {
		p.Initial = time.Second
	}
	if p.Maximum <= 0 {
		p.Maximum = time.Minute
	}
	if p.Maximum < p.Initial {
		p.Maximum = p.Initial
	}
	if p.Multiplier < 1 {
		p.Multiplier = 2
	}
	if p.Jitter <= 0 || p.Jitter > 1 {
		p.Jitter = .2
	}
	if p.Rand == nil {
		p.Rand = rand.Float64
	}
	if p.Sleep == nil {
		p.Sleep = sleepContext
	}
	return p
}

// Delay returns the capped jittered delay for a zero-based retry attempt.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	p = p.normalized()
	base := float64(p.Initial) * math.Pow(p.Multiplier, float64(attempt))
	if base > float64(p.Maximum) || math.IsInf(base, 0) {
		base = float64(p.Maximum)
	}
	factor := 1 - p.Jitter + 2*p.Jitter*p.Rand()
	delay := time.Duration(base * factor)
	if delay > p.Maximum {
		delay = p.Maximum
	}
	if delay < 0 {
		return 0
	}
	return delay
}

// RunReconnect repeatedly invokes connect with bounded delay until cancelled.
func RunReconnect(ctx context.Context, policy RetryPolicy, connect func(context.Context) error) error {
	if connect == nil {
		return errors.New("Agent connector is required")
	}
	policy = policy.normalized()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = connect(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := policy.Sleep(ctx, policy.Delay(attempt)); err != nil {
			return err
		}
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
