package memory

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// EntryType is the memory scope (session / user / agent).
type EntryType string

const (
	EntryTypeSession EntryType = "session"
	EntryTypeUser    EntryType = "user"
	EntryTypeAgent   EntryType = "agent"
)

// Memory kinds extracted from conversations. Mirrors proto MemoryType.
const (
	MemoryKindUser      = "user"
	MemoryKindFeedback  = "feedback"
	MemoryKindProject   = "project"
	MemoryKindReference = "reference"
)

// AgentMemory is the gorm persistence object for a stored memory. The schema
// mirrors XiaoQinglong agent-frame's agent_memory table.
type AgentMemory struct {
	Ulid        string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	CreatedAt   int64  `gorm:"column:created_at;type:bigint" json:"created_at"`
	UpdatedAt   int64  `gorm:"column:updated_at;type:bigint" json:"updated_at"`
	DeletedAt   int64  `gorm:"column:deleted_at;type:bigint;index" json:"deleted_at"`
	AgentId     string `gorm:"column:agent_id;type:varchar(128);index" json:"agent_id"`
	UserId      string `gorm:"column:user_id;type:varchar(128);index" json:"user_id"`
	SessionId   string `gorm:"column:session_id;type:varchar(128);index" json:"session_id"`
	EntryType   string `gorm:"column:entry_type;type:varchar(20);index" json:"entry_type"`
	MemoryType  string `gorm:"column:memory_type;type:varchar(50)" json:"memory_type"`
	MemoryKey   string `gorm:"column:memory_key;type:varchar(255);index" json:"memory_key"`
	Name        string `gorm:"column:name;type:varchar(255)" json:"name"`
	Description string `gorm:"column:description;type:varchar(500)" json:"description"`
	Content     string `gorm:"column:content;type:text" json:"content"`
	Keywords    string `gorm:"column:keywords;type:text" json:"keywords"`
	Importance  int    `gorm:"column:importance;type:int;default:1" json:"importance"`
	Source      string `gorm:"column:source;type:varchar(50)" json:"source"`
	SourceMsgId string `gorm:"column:source_msg_id;type:varchar(128)" json:"source_msg_id"`
	ExpiresAt   int64  `gorm:"column:expires_at;type:bigint" json:"expires_at"`
}

// TableName sets the gorm table name.
func (AgentMemory) TableName() string { return "agent_memory" }

// BeforeCreate assigns a ULID-like id and timestamps.
func (po *AgentMemory) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = newID()
	}
	now := time.Now().UnixMilli()
	if po.CreatedAt == 0 {
		po.CreatedAt = now
	}
	po.UpdatedAt = now
	return nil
}

// MemoryIndex is a lightweight retrieval index row, replacing the MEMORY.md
// text index used by the file-based runner.
type MemoryIndex struct {
	Ulid       string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	MemoryID   string `gorm:"column:memory_id;type:varchar(128);index" json:"memory_id"`
	HookLine   string `gorm:"column:hook_line;type:varchar(500)" json:"hook_line"`
	MemoryType string `gorm:"column:memory_type;type:varchar(50);index" json:"memory_type"`
	AgentId    string `gorm:"column:agent_id;type:varchar(128);index" json:"agent_id"`
	UserId     string `gorm:"column:user_id;type:varchar(128);index" json:"user_id"`
	CreatedAt  int64  `gorm:"column:created_at;type:bigint" json:"created_at"`
	UpdatedAt  int64  `gorm:"column:updated_at;type:bigint" json:"updated_at"`
}

// TableName sets the gorm table name.
func (MemoryIndex) TableName() string { return "memory_index" }

// BeforeCreate assigns an id and timestamps.
func (po *MemoryIndex) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = newID()
	}
	now := time.Now().UnixMilli()
	if po.CreatedAt == 0 {
		po.CreatedAt = now
	}
	po.UpdatedAt = now
	return nil
}

// newID returns a random hex id (ULID substitute without external deps).
func newID() string {
	ulidBytes := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).Bytes()
	return hex.EncodeToString(ulidBytes)
}
