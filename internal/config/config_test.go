package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverEnv is everything Load() insists on. The QA and seeder loaders are
// asserted against subsets of it.
var serverEnv = map[string]string{
	"DATABASE_URL":        "postgres://marco:marco@localhost:5432/marco_dev?sslmode=disable",
	"GOOGLE_CLIENT_ID":    "client-id.apps.googleusercontent.com",
	"JWT_SECRET":          "0123456789abcdef0123456789abcdef",
	"ANTHROPIC_API_KEY":   "sk-ant-test",
	"SENDGRID_API_KEY":    "SG.test",
	"SENDGRID_FROM_EMAIL": "marco@example.com",
}

// setEnv installs exactly the given variables and clears every other one this
// package reads, so a developer's real .env cannot make a case pass.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	all := []string{
		"PORT", "DATABASE_URL", "GOOGLE_CLIENT_ID", "JWT_SECRET",
		"JWT_ACCESS_TTL", "JWT_REFRESH_TTL", "ANTHROPIC_API_KEY",
		"SENDGRID_API_KEY", "SENDGRID_FROM_EMAIL", "SENDGRID_FROM_NAME",
		"SENDGRID_BASE_URL", "CURRICULUM_PATH",
	}
	for _, k := range all {
		t.Setenv(k, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func withoutKey(base map[string]string, drop string) map[string]string {
	out := make(map[string]string, len(base))
	for k, v := range base {
		if k != drop {
			out[k] = v
		}
	}
	return out
}

func TestLoad_RequiresEveryRuntimeSecret(t *testing.T) {
	tests := []struct {
		name    string
		missing string
		wantErr string
	}{
		{"database url", "DATABASE_URL", "DATABASE_URL is required"},
		{"google client id", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_ID is required"},
		{"jwt secret", "JWT_SECRET", "JWT_SECRET is required"},
		{"anthropic key", "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY is required"},
		{"sendgrid key", "SENDGRID_API_KEY", "SENDGRID_API_KEY is required"},
		{"sendgrid from", "SENDGRID_FROM_EMAIL", "SENDGRID_FROM_EMAIL is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, withoutKey(serverEnv, tt.missing))
			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// The QA harness talks to an already running server over HTTP. It reaches the
// database to load fixtures and signs its own access tokens, and that is all —
// so requiring the server's credentials broke `make qa` with
// "SENDGRID_API_KEY is required" for a harness that sends no email.
func TestLoadQA_NeedsOnlyDatabaseAndJWT(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": serverEnv["DATABASE_URL"],
		"JWT_SECRET":   serverEnv["JWT_SECRET"],
	})

	cfg, err := LoadQA()
	require.NoError(t, err)
	assert.Equal(t, serverEnv["DATABASE_URL"], cfg.DatabaseURL)
	assert.Equal(t, serverEnv["JWT_SECRET"], cfg.JWTSecret)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, 15*time.Minute, cfg.JWTAccessTTL)
	assert.Equal(t, 30*24*time.Hour, cfg.JWTRefreshTTL)
}

func TestLoadQA_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "no database url",
			env:     map[string]string{"JWT_SECRET": serverEnv["JWT_SECRET"]},
			wantErr: "DATABASE_URL is required",
		},
		{
			name:    "no jwt secret",
			env:     map[string]string{"DATABASE_URL": serverEnv["DATABASE_URL"]},
			wantErr: "JWT_SECRET is required",
		},
		{
			// The harness signs its own tokens, so a short or mismatched secret
			// surfaces as a wall of 401s rather than a config error.
			name: "jwt secret too short",
			env: map[string]string{
				"DATABASE_URL": serverEnv["DATABASE_URL"],
				"JWT_SECRET":   "too-short",
			},
			wantErr: "at least 32 characters",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)
			_, err := LoadQA()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadQA_HonoursTTLOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL":     serverEnv["DATABASE_URL"],
		"JWT_SECRET":       serverEnv["JWT_SECRET"],
		"JWT_ACCESS_TTL":   "5m",
		"JWT_REFRESH_TTL":  "48h",
		"PORT":             "3143",
		"SENDGRID_API_KEY": "",
	})

	cfg, err := LoadQA()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, cfg.JWTAccessTTL)
	assert.Equal(t, 48*time.Hour, cfg.JWTRefreshTTL)
	assert.Equal(t, "3143", cfg.Port)
}

// Same shape as LoadQA: seeding needs the database and nothing else.
func TestLoadSeeder_NeedsOnlyDatabaseURL(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": serverEnv["DATABASE_URL"]})

	cfg, err := LoadSeeder()
	require.NoError(t, err)
	assert.Equal(t, serverEnv["DATABASE_URL"], cfg.DatabaseURL)
	assert.Empty(t, cfg.CurriculumPath)
}

func TestLoadSeeder_RequiresDatabaseURL(t *testing.T) {
	setEnv(t, map[string]string{})

	_, err := LoadSeeder()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}
