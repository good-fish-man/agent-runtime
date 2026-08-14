package provider

import (
	"context"
	"strings"
)

type invocationScope struct {
	OwnerID string
	TaskID  string
}

type invocationScopeKey struct{}

// WithInvocationScope binds user/task provenance to every Provider call made
// by the current Agent run. Provider code cannot mutate this trusted context.
func WithInvocationScope(ctx context.Context, ownerID, taskID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, invocationScopeKey{}, invocationScope{
		OwnerID: strings.TrimSpace(ownerID),
		TaskID:  strings.TrimSpace(taskID),
	})
}

func invocationScopeFromContext(ctx context.Context) invocationScope {
	if ctx == nil {
		return invocationScope{}
	}
	value, _ := ctx.Value(invocationScopeKey{}).(invocationScope)
	return value
}
