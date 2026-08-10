package searchsystem

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrProviderCircuitOpen = errors.New("provider circuit is open")

type ResilienceConfig struct {
	Timeout          time.Duration
	FailureThreshold int
	OpenDuration     time.Duration
}

func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{Timeout: 10 * time.Second, FailureThreshold: 3, OpenDuration: 2 * time.Minute}
}

type resilientProvider struct {
	provider Provider
	config   ResilienceConfig

	mu                  sync.Mutex
	consecutiveFailures int
	openUntil           time.Time
}

func WithResilience(provider Provider, config ResilienceConfig) Provider {
	defaults := DefaultResilienceConfig()
	if config.Timeout <= 0 {
		config.Timeout = defaults.Timeout
	}
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = defaults.FailureThreshold
	}
	if config.OpenDuration <= 0 {
		config.OpenDuration = defaults.OpenDuration
	}
	return &resilientProvider{provider: provider, config: config}
}

func (p *resilientProvider) Name() string     { return p.provider.Name() }
func (p *resilientProvider) Kind() SourceKind { return p.provider.Kind() }

func (p *resilientProvider) Search(ctx context.Context, query Query, count int) ([]Hit, error) {
	p.mu.Lock()
	if time.Now().Before(p.openUntil) {
		openUntil := p.openUntil
		p.mu.Unlock()
		return nil, fmt.Errorf("%w for %s until %s", ErrProviderCircuitOpen, p.Name(), openUntil.Format(time.RFC3339))
	}
	p.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	hits, err := p.provider.Search(callCtx, query, count)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.consecutiveFailures = 0
		p.openUntil = time.Time{}
		return hits, nil
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil, err
	}
	p.consecutiveFailures++
	if p.consecutiveFailures >= p.config.FailureThreshold {
		p.openUntil = time.Now().Add(p.config.OpenDuration)
	}
	return nil, err
}
