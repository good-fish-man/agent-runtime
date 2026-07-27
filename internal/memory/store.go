package memory

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// MemoryEntry is an in-memory view of a stored memory used to build snapshots.
type MemoryEntry struct {
	Type        EntryType
	ID          string // session_id / user_id / agent_id
	Key         string
	Name        string
	Description string
	MemoryKind  string // user | feedback | project | reference
	Content     string
	Importance  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ExtractedMemory is a memory candidate produced by the extractor.
type ExtractedMemory struct {
	Name        string
	Description string
	Type        string // user | feedback | project | reference
	Content     string
	Importance  int
}

// MemStore persists memories in PostgreSQL and keeps per-scope snapshots in
// memory for fast system-prompt injection. It is a DB-backed port of the
// XiaoQinglong runner MemStore.
type MemStore struct {
	mu sync.RWMutex
	db *gorm.DB

	// frozen snapshots captured at initialization, keyed by scope id.
	sessionSnapshots map[string]string
	userSnapshots    map[string]string
	agentSnapshots   map[string]string

	injectionPatterns []*regexp.Regexp
}

// NewMemStore creates a DB-backed memory store.
func NewMemStore(db *gorm.DB) *MemStore {
	return &MemStore{
		db:               db,
		sessionSnapshots: make(map[string]string),
		userSnapshots:    make(map[string]string),
		agentSnapshots:   make(map[string]string),
		injectionPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)ignore\s+(previous|all)\s+instructions`),
			regexp.MustCompile(`(?i)you\s+are\s+now\s+(a|an)`),
			regexp.MustCompile(`(?i)disregard\s+(previous|all)\s+(instructions?|rules?)`),
			regexp.MustCompile(`\$[A-Z_]+\s*=.*curl|wget`),
			regexp.MustCompile(`authorized_keys`),
			regexp.MustCompile(`(?i)\.ssh/`),
		},
	}
}

// AutoMigrate creates/updates the memory tables.
func (s *MemStore) AutoMigrate() error {
	if s.db == nil {
		return fmt.Errorf("memory store has no database")
	}
	return s.db.AutoMigrate(&AgentMemory{}, &MemoryIndex{})
}

// ============ initialization (load frozen snapshots) ============

// InitializeSession loads a session's memories and freezes its snapshot.
func (s *MemStore) InitializeSession(ctx context.Context, sessionID string) error {
	entries, err := s.loadEntries(ctx, EntryTypeSession, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.sessionSnapshots[sessionID] = buildSnapshot(entries)
	s.mu.Unlock()
	return nil
}

// InitializeUser loads a user's memories and freezes its snapshot.
func (s *MemStore) InitializeUser(ctx context.Context, userID string) error {
	entries, err := s.loadEntries(ctx, EntryTypeUser, userID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.userSnapshots[userID] = buildSnapshot(entries)
	s.mu.Unlock()
	return nil
}

// InitializeAgent loads an agent's memories and freezes its snapshot.
func (s *MemStore) InitializeAgent(ctx context.Context, agentID string) error {
	entries, err := s.loadEntries(ctx, EntryTypeAgent, agentID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.agentSnapshots[agentID] = buildSnapshot(entries)
	s.mu.Unlock()
	return nil
}

// InitializeAll initializes any non-empty scopes.
func (s *MemStore) InitializeAll(ctx context.Context, sessionID, userID, agentID string) error {
	if sessionID != "" {
		if err := s.InitializeSession(ctx, sessionID); err != nil {
			return err
		}
	}
	if userID != "" {
		if err := s.InitializeUser(ctx, userID); err != nil {
			return err
		}
	}
	if agentID != "" {
		if err := s.InitializeAgent(ctx, agentID); err != nil {
			return err
		}
	}
	return nil
}

// ============ snapshot getters ============

// GetSessionSnapshot returns a session's frozen snapshot.
func (s *MemStore) GetSessionSnapshot(sessionID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionSnapshots[sessionID]
}

// GetUserSnapshot returns a user's frozen snapshot.
func (s *MemStore) GetUserSnapshot(userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userSnapshots[userID]
}

// GetAgentSnapshot returns an agent's frozen snapshot.
func (s *MemStore) GetAgentSnapshot(agentID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentSnapshots[agentID]
}

// ============ writes ============

// Add stores a new memory entry after a security scan and duplicate check.
func (s *MemStore) Add(ctx context.Context, entry MemoryEntry) error {
	if s.db == nil {
		return fmt.Errorf("memory store has no database")
	}
	if s.scanContent(entry.Content) {
		return fmt.Errorf("memory content failed security scan")
	}

	var count int64
	q := s.scopeQuery(entry.Type, entry.ID).Where("memory_key = ?", entry.Key)
	if err := q.Model(&AgentMemory{}).Count(&count).Error; err != nil {
		return fmt.Errorf("dedupe check: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("duplicate key: %s", entry.Key)
	}

	po := &AgentMemory{
		EntryType:   string(entry.Type),
		MemoryKey:   entry.Key,
		MemoryType:  entry.MemoryKind,
		Name:        entry.Name,
		Description: entry.Description,
		Content:     entry.Content,
		Importance:  maxInt(entry.Importance, 1),
	}
	assignScope(po, entry.Type, entry.ID)
	if err := s.db.WithContext(ctx).Create(po).Error; err != nil {
		return fmt.Errorf("create memory: %w", err)
	}
	return s.refreshSnapshot(ctx, entry.Type, entry.ID)
}

// Replace updates an existing memory identified by scope + key.
func (s *MemStore) Replace(ctx context.Context, entry MemoryEntry) error {
	if s.db == nil {
		return fmt.Errorf("memory store has no database")
	}
	if s.scanContent(entry.Content) {
		return fmt.Errorf("memory content failed security scan")
	}
	res := s.scopeQuery(entry.Type, entry.ID).
		Where("memory_key = ?", entry.Key).
		Model(&AgentMemory{}).
		Updates(map[string]any{
			"name":       entry.Name,
			"content":    entry.Content,
			"updated_at": time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return fmt.Errorf("update memory: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("key not found: %s", entry.Key)
	}
	return s.refreshSnapshot(ctx, entry.Type, entry.ID)
}

// Remove deletes a memory identified by scope + key.
func (s *MemStore) Remove(ctx context.Context, entryType EntryType, id, key string) error {
	if s.db == nil {
		return fmt.Errorf("memory store has no database")
	}
	res := s.scopeQuery(entryType, id).
		Where("memory_key = ?", key).
		Delete(&AgentMemory{})
	if res.Error != nil {
		return fmt.Errorf("delete memory: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("key not found: %s", key)
	}
	return s.refreshSnapshot(ctx, entryType, id)
}

// ============ reads ============

// GetAll returns all memories for a scope.
func (s *MemStore) GetAll(ctx context.Context, entryType EntryType, id string) ([]MemoryEntry, error) {
	return s.loadEntries(ctx, entryType, id)
}

// Search returns memories whose content/key contains keyword.
func (s *MemStore) Search(ctx context.Context, entryType EntryType, id, keyword string) ([]MemoryEntry, error) {
	if s.db == nil {
		return nil, fmt.Errorf("memory store has no database")
	}
	var pos []AgentMemory
	like := "%" + keyword + "%"
	err := s.scopeQuery(entryType, id).
		Where("content LIKE ? OR memory_key LIKE ? OR name LIKE ?", like, like, like).
		Order("created_at ASC").
		Find(&pos).Error
	if err != nil {
		return nil, fmt.Errorf("search memory: %w", err)
	}
	return toEntries(pos), nil
}

// SaveExtracted persists extracted memories, routing them to the right scope.
// User-typed memories persist to the user scope (cross-session); the rest go to
// the session scope. Failures on individual entries are ignored so the batch
// makes best-effort progress.
func (s *MemStore) SaveExtracted(ctx context.Context, sessionID, userID, agentID string, mems []ExtractedMemory) error {
	if len(mems) == 0 {
		return nil
	}
	actualUser := userID
	if actualUser == "" {
		actualUser = sessionID
	}
	for _, m := range mems {
		entry := MemoryEntry{
			Key:         m.Name,
			Name:        m.Name,
			Description: m.Description,
			MemoryKind:  m.Type,
			Content:     m.Content,
			Importance:  m.Importance,
		}
		switch m.Type {
		case MemoryKindUser:
			entry.Type = EntryTypeUser
			entry.ID = actualUser
		default:
			entry.Type = EntryTypeSession
			entry.ID = sessionID
		}
		if entry.ID == "" {
			continue
		}
		_ = s.Add(ctx, entry)
	}
	return nil
}

// ============ internals ============

func (s *MemStore) scopeQuery(entryType EntryType, id string) *gorm.DB {
	q := s.db.Where("entry_type = ?", string(entryType))
	switch entryType {
	case EntryTypeSession:
		q = q.Where("session_id = ?", id)
	case EntryTypeUser:
		q = q.Where("user_id = ?", id)
	case EntryTypeAgent:
		q = q.Where("agent_id = ?", id)
	}
	return q
}

func assignScope(po *AgentMemory, entryType EntryType, id string) {
	switch entryType {
	case EntryTypeSession:
		po.SessionId = id
	case EntryTypeUser:
		po.UserId = id
	case EntryTypeAgent:
		po.AgentId = id
	}
}

func (s *MemStore) loadEntries(ctx context.Context, entryType EntryType, id string) ([]MemoryEntry, error) {
	if s.db == nil {
		return nil, fmt.Errorf("memory store has no database")
	}
	var pos []AgentMemory
	err := s.scopeQuery(entryType, id).
		WithContext(ctx).
		Order("created_at ASC").
		Find(&pos).Error
	if err != nil {
		return nil, fmt.Errorf("load memories: %w", err)
	}
	return toEntries(pos), nil
}

func (s *MemStore) refreshSnapshot(ctx context.Context, entryType EntryType, id string) error {
	entries, err := s.loadEntries(ctx, entryType, id)
	if err != nil {
		return err
	}
	snap := buildSnapshot(entries)
	s.mu.Lock()
	switch entryType {
	case EntryTypeSession:
		s.sessionSnapshots[id] = snap
	case EntryTypeUser:
		s.userSnapshots[id] = snap
	case EntryTypeAgent:
		s.agentSnapshots[id] = snap
	}
	s.mu.Unlock()
	return nil
}

func (s *MemStore) scanContent(content string) bool {
	for _, p := range s.injectionPatterns {
		if p.MatchString(content) {
			return true
		}
	}
	return false
}

func toEntries(pos []AgentMemory) []MemoryEntry {
	entries := make([]MemoryEntry, 0, len(pos))
	for _, po := range pos {
		id := po.SessionId
		et := EntryType(po.EntryType)
		switch et {
		case EntryTypeUser:
			id = po.UserId
		case EntryTypeAgent:
			id = po.AgentId
		}
		entries = append(entries, MemoryEntry{
			Type:        et,
			ID:          id,
			Key:         po.MemoryKey,
			Name:        po.Name,
			Description: po.Description,
			MemoryKind:  po.MemoryType,
			Content:     po.Content,
			Importance:  po.Importance,
			CreatedAt:   time.UnixMilli(po.CreatedAt),
			UpdatedAt:   time.UnixMilli(po.UpdatedAt),
		})
	}
	return entries
}

func buildSnapshot(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.Content
	}
	return strings.Join(parts, "\n§\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
