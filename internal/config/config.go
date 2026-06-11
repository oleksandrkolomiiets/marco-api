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
