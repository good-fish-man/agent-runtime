package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
)

type AuditSink interface {
	Record(context.Context, pluginv1.InvocationTrace) error
}

type JSONLAuditSink struct {
	path string
	mu   sync.Mutex
}

func NewJSONLAuditSink(path string) *JSONLAuditSink { return &JSONLAuditSink{path: path} }

func (s *JSONLAuditSink) Record(_ context.Context, trace pluginv1.InvocationTrace) error {
	if s == nil || s.path == "" {
		return fmt.Errorf("provider audit path is not configured")
	}
	data, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("marshal provider invocation audit: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create provider audit directory: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open provider audit file: %w", err)
	}
	writer := bufio.NewWriter(file)
	_, writeErr := writer.Write(append(data, '\n'))
	flushErr := writer.Flush()
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write provider audit: %w", writeErr)
	}
	if flushErr != nil {
		return fmt.Errorf("flush provider audit: %w", flushErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close provider audit: %w", closeErr)
	}
	return nil
}
