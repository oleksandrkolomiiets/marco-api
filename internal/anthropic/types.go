package anthropic

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type StreamRequest struct {
	System    string
	Messages  []Message
	MaxTokens int
}

type StreamChunk struct {
	Text      string
	IsDone    bool
	FinalText string
}
