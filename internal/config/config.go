package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port            string
	DatabaseURL     string
	GoogleClientID  string
	JWTSecret       string
	JWTAccessTTL    time.Duration
	JWTRefreshTTL   time.Duration
	AnthropicAPIKey string

	SendGridAPIKey    string
	SendGridFromEmail string
	SendGridFromName  string
	// SendGridBaseURL overrides SendGrid's host. Left empty in every real
	// deployment; it exists so a local run can point at a stand-in instead of
	// mailing people from a dev machine.
	SendGridBaseURL string
}

// SeederConfig is a minimal config for the seeder binary — only the fields
// strictly needed to connect to the database and locate the curriculum file.
// Avoids requiring runtime-only env vars (JWT, Google, Anthropic) just to seed.
type SeederConfig struct {
	DatabaseURL    string
	CurriculumPath string
}

// LoadSeeder loads the seeder-only env (DATABASE_URL plus optional
// CURRICULUM_PATH). DATABASE_URL is required; the curriculum path can also
// be supplied on the command line and is therefore not required here.
func LoadSeeder() (*SeederConfig, error) {
	_ = godotenv.Load()

	cfg := &SeederConfig{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		CurriculumPath: os.Getenv("CURRICULUM_PATH"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

// QAConfig is a minimal config for the QA harness. It talks to an already
// running server over HTTP and only needs to reach the database (to load
// fixtures) and mint access tokens for the fixture users.
type QAConfig struct {
	Port          string
	DatabaseURL   string
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
}

// LoadQA loads the QA-only env. Same reasoning as LoadSeeder: the harness used
// the server's Load(), so adding a required variable to the server broke a
// tool that never touches it — `make qa` started failing with
// "SENDGRID_API_KEY is required" on every machine whose .env predated the
// password-reset work, for a harness that sends no email. The server it calls
// is what needs Google, Anthropic and SendGrid credentials.
func LoadQA() (*QAConfig, error) {
	_ = godotenv.Load()

	accessTTL, err := parseDuration("JWT_ACCESS_TTL", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	refreshTTL, err := parseDuration("JWT_REFRESH_TTL", 30*24*time.Hour)
	if err != nil {
		return nil, err
	}

	cfg := &QAConfig{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		JWTAccessTTL:  accessTTL,
		JWTRefreshTTL: refreshTTL,
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	// The harness signs its own tokens, so a mismatch here means every request
	// comes back 401 rather than anything that looks like a config problem.
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET is required and must be at least 32 characters")
	}
	return cfg, nil
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	accessTTL, err := parseDuration("JWT_ACCESS_TTL", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	refreshTTL, err := parseDuration("JWT_REFRESH_TTL", 30*24*time.Hour)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		GoogleClientID:  os.Getenv("GOOGLE_CLIENT_ID"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		JWTAccessTTL:    accessTTL,
		JWTRefreshTTL:   refreshTTL,
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),

		SendGridAPIKey:    os.Getenv("SENDGRID_API_KEY"),
		SendGridFromEmail: os.Getenv("SENDGRID_FROM_EMAIL"),
		SendGridFromName:  getEnv("SENDGRID_FROM_NAME", "Marco"),
		SendGridBaseURL:   os.Getenv("SENDGRID_BASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.GoogleClientID == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET is required and must be at least 32 characters")
	}
	if cfg.AnthropicAPIKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	if cfg.SendGridAPIKey == "" {
		return nil, fmt.Errorf("SENDGRID_API_KEY is required")
	}
	// SendGrid rejects a send whose From address isn't a verified sender
	// identity, and it does so at send time with a 403 — long after boot. Fail
	// at startup instead, where whoever deployed it is still watching.
	if cfg.SendGridFromEmail == "" {
		return nil, fmt.Errorf("SENDGRID_FROM_EMAIL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return d, nil
}
