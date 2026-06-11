package marco

import (
	"encoding/json"
	"regexp"
	"strings"
)

// MatchLogToken holds the structured data Marco emits when the user reports a match.
// All fields except Result and PlayedOn are optional — the parser treats empty strings
// as absent so the caller can decide whether to pass nil to the DB.
type MatchLogToken struct {
	Result      string   `json:"result"`              // "won", "lost", or "draw"
	PlayedOn    string   `json:"played_on"`           // YYYY-MM-DD
	Note        string   `json:"note"`                // score or free-text match note
	Feeling     string   `json:"feeling"`             // ≤50 chars
	PartnerName string   `json:"partner_name"`        // user's teammate (their side)
	Opponents   []string `json:"opponents,omitempty"` // names on the OTHER side
}

// matchLogRegex matches [MATCH_LOG: {...}] where the JSON object contains no
// nested braces — which is all we emit from the prompt.
var matchLogRegex = regexp.MustCompile(`\[MATCH_LOG:\s*(\{[^}]+\})\s*\]`)

// ParseMatchLogToken extracts the first [MATCH_LOG: ...] token from text and
// returns the decoded struct. Returns nil if no valid token is found.
func ParseMatchLogToken(text string) *MatchLogToken {
	m := matchLogRegex.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	var t MatchLogToken
	if err := json.Unmarshal([]byte(m[1]), &t); err != nil {
		return nil
	}
	return &t
}

// CleanMatchLogTokens removes all [MATCH_LOG: ...] tokens from text.
func CleanMatchLogTokens(text string) string {
	return strings.TrimSpace(matchLogRegex.ReplaceAllString(text, ""))
}
