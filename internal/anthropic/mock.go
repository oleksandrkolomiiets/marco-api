package anthropic

import (
	"context"
	"sync"
)

// MockClient is a test double that scripts a fixed sequence of chunks and an
// optional terminal error. It lives in a non-test file so callers outside the
// anthropic package (e.g. internal/chat tests) can import it.
type MockClient struct {
	mu     sync.Mutex
	chunks []StreamChunk
	err    error
}

func (m *MockClient) Setup(chunks []StreamChunk, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks = chunks
	m.err = err
}

func (m *MockClient) Stream(ctx context.Context, _ StreamRequest) (<-chan StreamChunk, <-chan error) {
	m.mu.Lock()
	chunks := append([]StreamChunk(nil), m.chunks...)
	err := m.err
	m.mu.Unlock()

	chunksCh := make(chan StreamChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunksCh)
		defer close(errCh)

		for _, c := range chunks {
			select {
			case chunksCh <- c:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}
	}()

	return chunksCh, errCh
}

var _ Client = (*MockClient)(nil)
