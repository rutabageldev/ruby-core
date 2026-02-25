package main

import (
	"fmt"
	"os"
	"sync"
)

// NDJSONWriter appends raw event bytes as newline-delimited JSON to a file.
// Each call to Write appends one line (the caller's data + "\n").
// The file is opened in append mode and synced after each write for durability.
// It is safe for concurrent use.
type NDJSONWriter struct {
	f  *os.File
	mu sync.Mutex
}

// NewNDJSONWriter opens (or creates) the file at path in append mode.
func NewNDJSONWriter(path string) (*NDJSONWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: path is operator-controlled (AUDIT_DATA_DIR env + constant filename)
	if err != nil {
		return nil, fmt.Errorf("audit-sink: open %q: %w", path, err)
	}
	return &NDJSONWriter{f: f}, nil
}

// Write appends data followed by a newline to the file and syncs to disk.
func (w *NDJSONWriter) Write(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.f.Write(data); err != nil {
		return fmt.Errorf("audit-sink: write: %w", err)
	}
	if _, err := w.f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("audit-sink: write newline: %w", err)
	}
	// Sync ensures the record is durable before we ACK the NATS message.
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("audit-sink: sync: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file.
func (w *NDJSONWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
