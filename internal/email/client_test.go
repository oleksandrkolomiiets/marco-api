package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capture stands in for SendGrid. It records the one request it receives and
// answers with the status the test asked for.
type capture struct {
	server *httptest.Server
	path   string
	auth   string
	body   sendRequest
	raw    string
	calls  int
}

func newCapture(t *testing.T, status int, respBody string) *capture {
	t.Helper()
	c := &capture{}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.calls++
		c.path = r.URL.Path
		c.auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		c.raw = string(raw)
		_ = json.Unmarshal(raw, &c.body)
		w.WriteHeader(status)
		if respBody != "" {
			_, _ = w.Write([]byte(respBody))
		}
	}))
	t.Cleanup(c.server.Close)
	return c
}

func newTestSender(t *testing.T, c *capture) Sender {
	t.Helper()
	return NewSendGridSender(Config{
		APIKey:    "SG.test-key",
		FromEmail: "marco@example.com",
		FromName:  "Marco",
		BaseURL:   c.server.URL,
	})
}

func TestSendPasswordReset_PostsTheSendGridPayload(t *testing.T) {
	c := newCapture(t, http.StatusAccepted, "")
	sender := newTestSender(t, c)

	err := sender.SendPasswordReset(
		context.Background(), "player@example.com", "Ana", "004271", 15*time.Minute,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, c.calls)
	assert.Equal(t, "/v3/mail/send", c.path)
	assert.Equal(t, "Bearer SG.test-key", c.auth)

	require.Len(t, c.body.Personalizations, 1)
	require.Len(t, c.body.Personalizations[0].To, 1)
	assert.Equal(t, "player@example.com", c.body.Personalizations[0].To[0].Email)
	assert.Equal(t, "Ana", c.body.Personalizations[0].To[0].Name)
	assert.Equal(t, "marco@example.com", c.body.From.Email)
	assert.Equal(t, "Marco", c.body.From.Name)
	assert.Equal(t, passwordResetSubject, c.body.Subject)

	require.Len(t, c.body.Content, 1)
	body := c.body.Content[0].Value
	assert.Equal(t, "text/plain", c.body.Content[0].Type)
	assert.Contains(t, body, "004271", "the code has to reach the reader")
	assert.Contains(t, body, "Hola Ana,")
	assert.Contains(t, body, "15 minutes", "the stated expiry tracks PasswordResetTTL")
}

// A leading zero is a third of a percent of all codes and the one case where
// treating the code as a number instead of a string silently mangles it.
func TestSendPasswordReset_KeepsLeadingZeros(t *testing.T) {
	c := newCapture(t, http.StatusAccepted, "")
	sender := newTestSender(t, c)

	require.NoError(t, sender.SendPasswordReset(
		context.Background(), "p@example.com", "", "000042", 15*time.Minute,
	))
	assert.Contains(t, c.body.Content[0].Value, "000042")
}

func TestSendPasswordReset_OmitsTheNameWhenThereIsntOne(t *testing.T) {
	c := newCapture(t, http.StatusAccepted, "")
	sender := newTestSender(t, c)

	require.NoError(t, sender.SendPasswordReset(
		context.Background(), "p@example.com", "", "123456", 15*time.Minute,
	))
	assert.Contains(t, c.body.Content[0].Value, "Hola,")
	assert.NotContains(t, c.body.Content[0].Value, "Hola ,")
}

func TestSendPasswordResetForGoogleAccount_SendsNoCode(t *testing.T) {
	c := newCapture(t, http.StatusAccepted, "")
	sender := newTestSender(t, c)

	require.NoError(t, sender.SendPasswordResetForGoogleAccount(
		context.Background(), "g@example.com", "Ana",
	))
	assert.Equal(t, googleAccountSubject, c.body.Subject)
	assert.Contains(t, c.body.Content[0].Value, "Google")
	assert.NotContains(t, c.body.Content[0].Value, "code to reset")
}

// A wrong API key and an unverified sender both come back as a non-202 with a
// JSON errors array. Losing that detail leaves the operator with nothing.
func TestSend_SurfacesSendGridsRejection(t *testing.T) {
	c := newCapture(t, http.StatusForbidden,
		`{"errors":[{"message":"The from address does not match a verified Sender Identity."}]}`)
	sender := newTestSender(t, c)

	err := sender.SendPasswordReset(
		context.Background(), "p@example.com", "Ana", "123456", 15*time.Minute,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "verified Sender Identity")
}

func TestSend_ReturnsTheTransportError(t *testing.T) {
	c := newCapture(t, http.StatusAccepted, "")
	sender := newTestSender(t, c)
	c.server.Close() // nothing listening any more

	err := sender.SendPasswordReset(
		context.Background(), "p@example.com", "Ana", "123456", 15*time.Minute,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send via sendgrid")
}

func TestNewSendGridSender_DefaultsToTheRealHost(t *testing.T) {
	s, ok := NewSendGridSender(Config{APIKey: "k", FromEmail: "f@x.com"}).(*sendgridSender)
	require.True(t, ok)
	assert.Equal(t, "https://api.sendgrid.com", s.baseURL)
}

// A trailing slash on the override would otherwise produce "//v3/mail/send".
func TestNewSendGridSender_TrimsTrailingSlash(t *testing.T) {
	s, ok := NewSendGridSender(Config{BaseURL: "http://localhost:9999/"}).(*sendgridSender)
	require.True(t, ok)
	assert.Equal(t, "http://localhost:9999", s.baseURL)
}

func TestHumanTTL(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"under a minute rounds up", 30 * time.Second, "1 minute"},
		{"one minute", time.Minute, "1 minute"},
		{"the default", 15 * time.Minute, "15 minutes"},
		{"exactly an hour", time.Hour, "1 hour"},
		{"more than an hour", 2 * time.Hour, "2 hours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, humanTTL(tt.in))
		})
	}
}

// The plain-text body is what lands in the inbox; a stray %!s(MISSING) from a
// bad format string would ship straight to a user.
func TestBodies_HaveNoFormattingArtefacts(t *testing.T) {
	bodies := []string{
		passwordResetBody("Ana", "123456", 15*time.Minute),
		passwordResetBody("", "123456", 15*time.Minute),
		googleAccountBody("Ana"),
		googleAccountBody(""),
	}
	for _, b := range bodies {
		assert.NotContains(t, b, "%!")
		assert.NotContains(t, b, "MISSING")
		assert.False(t, strings.Contains(b, "<no value>"))
	}
}
