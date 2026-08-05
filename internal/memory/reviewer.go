package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	log "github.com/good-fish-man/logx"
)

// ReviewConfig configures the background reviewer.
type ReviewConfig struct {
	Enabled   bool
	MaxMemory int
}

// BackgroundReviewer mines and persists memories after the main response has
// been produced, so it never competes with the primary task. It mirrors the
// XiaoQinglong _spawn_background_review pattern.
type BackgroundReviewer struct {
	cfg   ReviewConfig
	store *MemStore

	mu     sync.Mutex
	active bool
}

// NewBackgroundReviewer creates a reviewer bound to a store.
func NewBackgroundReviewer(cfg ReviewConfig, store *MemStore) *BackgroundReviewer {
	if cfg.MaxMemory <= 0 {
		cfg.MaxMemory = constant.DefaultMaxReviewMemory
	}
	return &BackgroundReviewer{cfg: cfg, store: store}
}

// ReviewIfNeeded launches an async review when the exchange looks worth
// remembering. It returns immediately.
func (r *BackgroundReviewer) ReviewIfNeeded(model eino.ModelConfig, sessionID, userID, agentID, userInput, assistantOutput string) {
	if r == nil || !r.cfg.Enabled || r.store == nil {
		return
	}
	if !shouldTriggerReview(userInput, assistantOutput) {
		return
	}

	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return
	}
	r.active = true
	r.mu.Unlock()

	log.Go(func() {
		defer func() {
			r.mu.Lock()
			r.active = false
			r.mu.Unlock()
		}()

		ctx := context.Background()
		extractor := NewExtractor(model)
		mems, err := extractor.Extract(ctx, userInput, assistantOutput)
		if err != nil {
			log.Errorf("[memory] background extract failed: %v", err)
			return
		}
		if len(mems) == 0 {
			return
		}
		if len(mems) > r.cfg.MaxMemory {
			mems = mems[:r.cfg.MaxMemory]
		}
		if err := r.store.SaveExtracted(ctx, sessionID, userID, agentID, mems); err != nil {
			log.Errorf("[memory] background save failed: %v", err)
			return
		}
		// Refresh session snapshot so subsequent turns see new memories.
		if sessionID != "" {
			_ = r.store.InitializeSession(ctx, sessionID)
		}
		log.Infof("[memory] background review saved %d memories (session=%s)", len(mems), sessionID)
	})
}

// shouldTriggerReview decides whether an exchange warrants extraction.
func shouldTriggerReview(userInput, assistantOutput string) bool {
	if len(assistantOutput) < 100 {
		return false
	}
	combined := strings.ToLower(userInput + assistantOutput)
	keywords := []string{
		"记住", "以后要", "下次记得",
		"save", "remember", "keep in mind",
		"don't forget", "never",
	}
	for _, kw := range keywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return len(assistantOutput) > 2000
}
