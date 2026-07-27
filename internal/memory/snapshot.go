package memory

import (
	"fmt"
	"strings"
)

const snapshotRule = "══════════════════════════════════════════════"

// FormatMemoryBlock formats a snapshot as a labelled block for the system prompt.
func FormatMemoryBlock(snapshot string, entryType EntryType) string {
	if snapshot == "" {
		return ""
	}
	header := "MEMORY"
	switch entryType {
	case EntryTypeSession:
		header = "SESSION MEMORY"
	case EntryTypeUser:
		header = "USER MEMORY"
	case EntryTypeAgent:
		header = "AGENT MEMORY"
	}
	return fmt.Sprintf("%s\n%s\n%s\n%s", snapshotRule, header, snapshotRule, snapshot)
}

// MemoryContext carries the frozen snapshots for a request's scopes.
type MemoryContext struct {
	SessionID   string
	UserID      string
	AgentID     string
	SessionSnap string
	UserSnap    string
	AgentSnap   string
}

// NewMemoryContext builds a MemoryContext from the store's frozen snapshots.
func NewMemoryContext(sessionID, userID, agentID string, store *MemStore) *MemoryContext {
	mc := &MemoryContext{SessionID: sessionID, UserID: userID, AgentID: agentID}
	if store != nil {
		if sessionID != "" {
			mc.SessionSnap = store.GetSessionSnapshot(sessionID)
		}
		if userID != "" {
			mc.UserSnap = store.GetUserSnapshot(userID)
		}
		if agentID != "" {
			mc.AgentSnap = store.GetAgentSnapshot(agentID)
		}
	}
	return mc
}

// ToPromptBlock renders all non-empty scope snapshots as a single block.
func (m *MemoryContext) ToPromptBlock() string {
	var blocks []string
	if b := FormatMemoryBlock(m.SessionSnap, EntryTypeSession); b != "" {
		blocks = append(blocks, b)
	}
	if b := FormatMemoryBlock(m.UserSnap, EntryTypeUser); b != "" {
		blocks = append(blocks, b)
	}
	if b := FormatMemoryBlock(m.AgentSnap, EntryTypeAgent); b != "" {
		blocks = append(blocks, b)
	}
	return strings.Join(blocks, "\n\n")
}

// LoadMemorySection returns the formatted memory block for the scopes named in
// the request context map (session_id / user_id / agent_id).
func LoadMemorySection(store *MemStore, sessionID, userID, agentID string) string {
	if store == nil {
		return ""
	}
	return NewMemoryContext(sessionID, userID, agentID, store).ToPromptBlock()
}
