// --- START OF NEW FILE internal/shared/buffer.go ---
package shared

import (
	"bytes"
	"sync"
)

// ThreadSafeBuffer is a simple thread-safe bytes.Buffer wrapper
type ThreadSafeBuffer struct {
	b  bytes.Buffer
	mu sync.Mutex
}

// NewThreadSafeBuffer creates a new ThreadSafeBuffer
func NewThreadSafeBuffer() *ThreadSafeBuffer {
	return &ThreadSafeBuffer{}
}

// Read reads data from the buffer, is thread-safe
func (b *ThreadSafeBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Read(p)
}

// Write writes data to the buffer, is thread-safe
func (b *ThreadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// Len returns the number of bytes in the buffer, is thread-safe
func (b *ThreadSafeBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Len()
}
