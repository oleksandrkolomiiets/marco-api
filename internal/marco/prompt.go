package marco

import _ "embed"

//go:embed prompt.md
var systemPromptBase string

// SystemPrompt returns the base persona prompt with the user's context block appended.
// The context block is the JSON produced by AssembleContext, serialised and labeled
// so the model can locate it. Keeping context as JSON (not prose) helps the model
// treat it as structured data rather than ambient text.
func SystemPrompt(contextJSON string) string {
	return systemPromptBase + "\n\n---\n\nCURRENT USER CONTEXT (JSON):\n\n" + contextJSON
}
