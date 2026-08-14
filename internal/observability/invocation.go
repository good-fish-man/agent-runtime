package observability

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/good-fish-man/logx"
)

var invocationSequence atomic.Uint64

// Invocation records one externally observable model or tool call. It is safe
// to finish from a deferred cleanup path and guarantees exactly one end event.
type Invocation struct {
	ctx       context.Context
	kind      string
	name      string
	id        string
	startedAt time.Time
	base      []any
	once      sync.Once
}

// Begin writes the start event. A caller-provided ID is useful for preserving
// a model tool_call_id; an empty ID receives a process-unique value.
func Begin(ctx context.Context, kind, name, id string, fields ...any) *Invocation {
	startedAt := time.Now()
	if id == "" {
		id = fmt.Sprintf("%s-%x-%d", kind, startedAt.UnixMilli(), invocationSequence.Add(1))
	}
	invocation := &Invocation{
		ctx:       ctx,
		kind:      kind,
		name:      name,
		id:        id,
		startedAt: startedAt,
		base:      append([]any{"source", callerSource(1)}, fields...),
	}
	log.InfowCtx(ctx, kind+" call started", invocation.fields(startedAt, false, 0)...)
	return invocation
}

func callerSource(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}
	if cwd, err := os.Getwd(); err == nil {
		if relative, relErr := filepath.Rel(cwd, file); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			file = relative
		}
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(file), line)
}

// End writes exactly one completion or failure event for the invocation.
func (i *Invocation) End(err error, fields ...any) {
	if i == nil {
		return
	}
	i.once.Do(func() {
		finishedAt := time.Now()
		values := i.fields(finishedAt, true, finishedAt.Sub(i.startedAt), fields...)
		if err != nil {
			values = append(values, "error_chain", log.FormatError(err))
			log.ErrorwCtx(i.ctx, i.kind+" call failed", values...)
			return
		}
		log.InfowCtx(i.ctx, i.kind+" call completed", values...)
	})
}

func (i *Invocation) fields(at time.Time, completed bool, elapsed time.Duration, extra ...any) []any {
	values := make([]any, 0, 10+len(i.base)+len(extra))
	values = append(values,
		"span_name", invocationSpanName(i.kind),
		"span_id", i.id,
		"call_id", i.id,
		i.kind, i.name,
	)
	values = append(values, i.base...)
	if !completed {
		values = append(values, "started_at", at.UTC().Format(time.RFC3339Nano))
	} else {
		values = append(values,
			"finished_at", at.UTC().Format(time.RFC3339Nano),
			"cost_ms", elapsed.Milliseconds(),
			"cost_us", elapsed.Microseconds(),
		)
	}
	values = append(values, extra...)
	return values
}

func invocationSpanName(kind string) string {
	if kind == "model" {
		return "model.invoke"
	}
	return kind + ".invoke"
}
