// Package email is a thin sender over SendGrid's v3 Mail Send API, in the same
// spirit as internal/anthropic: one interface, one real implementation, and a
// mock in a non-test file so other packages' tests can script it.
//
// It talks to the endpoint directly with net/http rather than pulling in
// sendgrid-go, which is itself a wrapper around this one JSON POST.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Sender is what the auth handler depends on. Keeping it this narrow means the
// handler's tests never touch HTTP.
type Sender interface {
	// SendPasswordReset delivers a reset code. name may be empty.
	SendPasswordReset(ctx context.Context, to, name, code string, ttl time.Duration) error
	// SendPasswordResetForGoogleAccount tells someone who signed up with Google
	// that there is no password to reset. Without it, asking for a reset on a
	// Google account is a silent dead end: no email ever arrives and the generic
	// "check your inbox" response gives them nothing to act on.
	SendPasswordResetForGoogleAccount(ctx context.Context, to, name string) error
}

const (
	defaultBaseURL = "https://api.sendgrid.com"
	sendPath       = "/v3/mail/send"
	requestTimeout = 10 * time.Second
)

type sendgridSender struct {
	apiKey    string
	fromEmail string
	fromName  string
	baseURL   string
	http      *http.Client
}

// Config is the sender's slice of the app config.
type Config struct {
	APIKey    string
	FromEmail string
	FromName  string
	// BaseURL overrides SendGrid's host. Empty means the real API; the only
	// reason to set it is to point tests or a local run at a stand-in.
	BaseURL string
}

func NewSendGridSender(cfg Config) Sender {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &sendgridSender{
		apiKey:    cfg.APIKey,
		fromEmail: cfg.FromEmail,
		fromName:  cfg.FromName,
		baseURL:   strings.TrimRight(base, "/"),
		http:      &http.Client{Timeout: requestTimeout},
	}
}

// https://www.twilio.com/docs/sendgrid/api-reference/mail-send/mail-send
type sendRequest struct {
	Personalizations []personalization `json:"personalizations"`
	From             address           `json:"from"`
	Subject          string            `json:"subject"`
	Content          []content         `json:"content"`
}

type personalization struct {
	To []address `json:"to"`
}

type address struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type content struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (s *sendgridSender) SendPasswordReset(ctx context.Context, to, name, code string, ttl time.Duration) error {
	return s.send(ctx, to, name, passwordResetSubject, passwordResetBody(name, code, ttl))
}

func (s *sendgridSender) SendPasswordResetForGoogleAccount(ctx context.Context, to, name string) error {
	return s.send(ctx, to, name, googleAccountSubject, googleAccountBody(name))
}

func (s *sendgridSender) send(ctx context.Context, to, name, subject, body string) error {
	payload := sendRequest{
		Personalizations: []personalization{{To: []address{{Email: to, Name: name}}}},
		From:             address{Email: s.fromEmail, Name: s.fromName},
		Subject:          subject,
		Content:          []content{{Type: "text/plain", Value: body}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode sendgrid payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, s.baseURL+sendPath, bytes.NewReader(encoded),
	)
	if err != nil {
		return fmt.Errorf("build sendgrid request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("send via sendgrid: %w", err)
	}
	defer resp.Body.Close()

	// A successful send is 202 with an empty body. Anything else carries a JSON
	// errors array worth surfacing — a wrong API key and an unverified sender
	// look identical otherwise, and both are things only the operator can fix.
	if resp.StatusCode != http.StatusAccepted {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf(
			"sendgrid returned %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)),
		)
	}
	return nil
}
