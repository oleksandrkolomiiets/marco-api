package main

import (
	"context"
	"fmt"
	"os"

	"marco-api/internal/anthropic"
	"marco-api/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if cfg.AnthropicAPIKey == "" {
		fmt.Fprintln(os.Stderr, "ANTHROPIC_API_KEY is required")
		os.Exit(1)
	}

	client := anthropic.NewClient(cfg.AnthropicAPIKey)

	req := anthropic.StreamRequest{
		System: "You are Marco, a friendly padel coach. Reply in one sentence.",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: "Say hi in Spanish."},
		},
	}

	chunks, errCh := client.Stream(context.Background(), req)

	var finalText string
	for c := range chunks {
		if c.IsDone {
			finalText = c.FinalText
			fmt.Println()
			fmt.Printf("[done] final length: %d chars\n", len(finalText))
			continue
		}
		fmt.Print(c.Text)
	}

	if err, ok := <-errCh; ok && err != nil {
		fmt.Fprintf(os.Stderr, "\nstream error: %v\n", err)
		os.Exit(1)
	}
}
