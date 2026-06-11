package anthropic

import (
	"context"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultMaxTokens = 1024

type Client interface {
	Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, <-chan error)
}

type sdkClient struct {
	sdk   sdk.Client
	model string
}

func NewClient(apiKey string) Client {
	return &sdkClient{
		sdk:   sdk.NewClient(option.WithAPIKey(apiKey)),
		model: string(sdk.ModelClaudeSonnet4_6),
	}
}

func (c *sdkClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk)
	errCh := make(chan error, 1)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	messages := make([]sdk.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		block := sdk.NewTextBlock(m.Content)
		switch m.Role {
		case RoleAssistant:
			messages = append(messages, sdk.NewAssistantMessage(block))
		default:
			messages = append(messages, sdk.NewUserMessage(block))
		}
	}

	params := sdk.MessageNewParams{
		Model:     sdk.Model(c.model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}
	if req.System != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}

	go func() {
		defer close(chunks)
		defer close(errCh)

		stream := c.sdk.Messages.NewStreaming(ctx, params)
		defer stream.Close()

		var builder strings.Builder

		for stream.Next() {
			if ctx.Err() != nil {
				return
			}
			event := stream.Current()
			deltaEvent, ok := event.AsAny().(sdk.ContentBlockDeltaEvent)
			if !ok {
				continue
			}
			textDelta, ok := deltaEvent.Delta.AsAny().(sdk.TextDelta)
			if !ok {
				continue
			}
			if textDelta.Text == "" {
				continue
			}
			builder.WriteString(textDelta.Text)
			select {
			case chunks <- StreamChunk{Text: textDelta.Text}:
			case <-ctx.Done():
				return
			}
		}

		if err := stream.Err(); err != nil {
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
			return
		}

		select {
		case chunks <- StreamChunk{IsDone: true, FinalText: builder.String()}:
		case <-ctx.Done():
		}
	}()

	return chunks, errCh
}
