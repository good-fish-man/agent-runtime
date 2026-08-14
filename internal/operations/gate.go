// Package operations provides bounded admission, request deadlines, health,
// and SLO snapshots for the Runtime's public execution paths.
package operations

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

var (
	ErrOverloaded = errors.New("runtime admission queue is full")
	ErrDraining   = errors.New("runtime is draining")
)

type Config struct {
	MaxInflight       int
	MaxQueue          int
	AdmissionWait     time.Duration
	RequestTimeout    time.Duration
	LatencySampleSize int
}

type Gate struct {
	cfg       Config
	slots     chan struct{}
	startedAt time.Time
	instance  string
	draining  atomic.Bool
	drainOnce sync.Once
	drainCh   chan struct{}
	inflight  atomic.Int64
	queued    atomic.Int64
	requests  atomic.Int64
	errors    atomic.Int64
	rejected  atomic.Int64
	timedOut  atomic.Int64
	dropped   atomic.Int64
	latencyMu sync.Mutex
	latencies []int64
}

func NewGate(cfg Config, instance string) *Gate {
	if cfg.MaxInflight < 1 {
		cfg.MaxInflight = 32
	}
	if cfg.MaxQueue < 0 {
		cfg.MaxQueue = 0
	}
	if cfg.AdmissionWait <= 0 {
		cfg.AdmissionWait = 2 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 2 * time.Minute
	}
	if cfg.LatencySampleSize < 16 {
		cfg.LatencySampleSize = 2048
	}
	return &Gate{cfg: cfg, slots: make(chan struct{}, cfg.MaxInflight), drainCh: make(chan struct{}), startedAt: time.Now().UTC(), instance: instance}
}

func (g *Gate) Acquire(ctx context.Context) (context.Context, func(bool), error) {
	g.requests.Add(1)
	if err := ctx.Err(); err != nil {
		g.reject(err)
		return ctx, nil, err
	}
	if g.draining.Load() {
		g.reject(ErrDraining)
		return ctx, nil, ErrDraining
	}
	select {
	case g.slots <- struct{}{}:
		return g.finishAcquire(ctx)
	default:
	}
	if g.queued.Add(1) > int64(g.cfg.MaxQueue) {
		g.queued.Add(-1)
		g.reject(ErrOverloaded)
		return ctx, nil, ErrOverloaded
	}
	defer g.queued.Add(-1)
	waitCtx, cancelWait := context.WithTimeout(ctx, g.cfg.AdmissionWait)
	defer cancelWait()
	select {
	case g.slots <- struct{}{}:
		return g.finishAcquire(ctx)
	case <-g.drainCh:
		g.reject(ErrDraining)
		return ctx, nil, ErrDraining
	case <-waitCtx.Done():
		g.reject(waitCtx.Err())
		return ctx, nil, waitCtx.Err()
	}
}

func (g *Gate) finishAcquire(ctx context.Context) (context.Context, func(bool), error) {
	if err := ctx.Err(); err != nil {
		<-g.slots
		g.reject(err)
		return ctx, nil, err
	}
	if g.draining.Load() {
		<-g.slots
		g.reject(ErrDraining)
		return ctx, nil, ErrDraining
	}
	return g.acquired(ctx)
}

func (g *Gate) acquired(parent context.Context) (context.Context, func(bool), error) {
	g.inflight.Add(1)
	started := time.Now()
	runCtx, cancel := context.WithTimeout(parent, g.cfg.RequestTimeout)
	var once sync.Once
	release := func(failed bool) {
		once.Do(func() {
			cancel()
			<-g.slots
			g.inflight.Add(-1)
			timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
			if failed || timedOut {
				g.errors.Add(1)
			}
			if timedOut {
				g.timedOut.Add(1)
			}
			g.recordLatency(time.Since(started).Milliseconds())
		})
	}
	return runCtx, release, nil
}

func (g *Gate) Drain() {
	g.drainOnce.Do(func() {
		g.draining.Store(true)
		close(g.drainCh)
	})
}

func (g *Gate) reject(err error) {
	g.rejected.Add(1)
	g.errors.Add(1)
	if errors.Is(err, context.DeadlineExceeded) {
		g.timedOut.Add(1)
	}
}

// RecordDroppedEvents records actual event delivery loss. Admission rejection
// is tracked independently and must never be reported as an event loss.
func (g *Gate) RecordDroppedEvents(count int64) {
	if count > 0 {
		g.dropped.Add(count)
	}
}

func (g *Gate) Health(version string) operationsv1.HealthSnapshot {
	status := operationsv1.HealthHealthy
	message := ""
	if g.draining.Load() {
		status = operationsv1.HealthUnhealthy
		message = "runtime is draining"
	} else if g.queued.Load() > 0 {
		status = operationsv1.HealthDegraded
		message = "requests are waiting for admission"
	}
	return operationsv1.HealthSnapshot{
		Schema: operationsv1.Schema, Component: "agent-runtime", Version: version, InstanceID: g.instance,
		Status: status, UptimeMS: time.Since(g.startedAt).Milliseconds(), Inflight: g.inflight.Load(),
		QueueDepth: g.queued.Load(), ObservedAt: time.Now().UTC(),
		Checks: []operationsv1.HealthCheck{{Name: "admission", Status: status, Message: message}},
	}
}

func (g *Gate) SLO() operationsv1.SLOSnapshot {
	now := time.Now().UTC()
	requests, failures := g.requests.Load(), g.errors.Load()
	availability := 1.0
	if requests > 0 {
		availability = float64(requests-failures) / float64(requests)
	}
	return operationsv1.SLOSnapshot{
		Schema: operationsv1.Schema, Component: "agent-runtime", WindowStart: g.startedAt, WindowEnd: now,
		Requests: requests, Errors: failures, Availability: availability, P95LatencyMS: g.p95(),
		RejectedRequests: g.rejected.Load(), TimedOutRequests: g.timedOut.Load(), DroppedEvents: g.dropped.Load(),
	}
}

func (g *Gate) recordLatency(value int64) {
	g.latencyMu.Lock()
	defer g.latencyMu.Unlock()
	if len(g.latencies) == g.cfg.LatencySampleSize {
		copy(g.latencies, g.latencies[1:])
		g.latencies[len(g.latencies)-1] = value
		return
	}
	g.latencies = append(g.latencies, value)
}

func (g *Gate) p95() int64 {
	g.latencyMu.Lock()
	values := append([]int64(nil), g.latencies...)
	g.latencyMu.Unlock()
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := (len(values)*95 + 99) / 100
	if index < 1 {
		index = 1
	}
	return values[index-1]
}
