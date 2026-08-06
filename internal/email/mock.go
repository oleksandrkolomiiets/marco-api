package email

import (
	"context"
	"sync"
	"time"
)

// SentEmail is one captured delivery.
type SentEmail struct {
	To   string
	Name string
	// Code is empty for the Google-account notice, which carries no code.
	Code   string
	TTL    time.Duration
	Google bool
}

// MockSender records what would have been sent and can be told to fail. It
// lives in a non-test file so internal/auth's tests can use it, mirroring
// anthropic.MockClient.
type MockSender struct {
	mu   sync.Mutex
	sent []SentEmail
	err  error
}

// SetError makes every subsequent send fail with err. Pass nil to clear.
func (m *MockSender) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// Sent returns a copy of everything captured so far.
func (m *MockSender) Sent() []SentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SentEmail(nil), m.sent...)
}

// Last returns the most recent send, or nil if there hasn't been one.
func (m *MockSender) Last() *SentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return nil
	}
	last := m.sent[len(m.sent)-1]
	return &last
}

func (m *MockSender) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = nil
	m.err = nil
}

func (m *MockSender) SendPasswordReset(_ context.Context, to, name, code string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, SentEmail{To: to, Name: name, Code: code, TTL: ttl})
	return nil
}

func (m *MockSender) SendPasswordResetForGoogleAccount(_ context.Context, to, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, SentEmail{To: to, Name: name, Google: true})
	return nil
}
