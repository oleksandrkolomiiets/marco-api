package marco

import (
	"encoding/json"
	"strings"
)

// MatchPrepToken is the structured payload Marco emits when the user asks to
// adjust or create a match preparation. The chat UI reads it from an
// `event: match_prep` SSE frame (and from the stored assistant message on
// history reload) and opens the prep sliding sheet.
//
// Two modes are supported on the same envelope to keep the wire format aligned
// with [MATCH_LOG: ...]:
//   - mode="adjust"  → id is required; the sheet opens for that existing prep.
//   - mode="create"  → id is omitted; the remaining fields prefill a new prep
//     form. scheduled_at is required in create mode.
type MatchPrepToken struct {
	Mode        string           `json:"mode"`                   // "adjust" or "create"
	ID          string           `json:"id,omitempty"`           // existing preparation UUID (adjust mode)
	ScheduledAt string           `json:"scheduled_at,omitempty"` // RFC3339 timestamp (create mode)
	Opponents   []string         `json:"opponents,omitempty"`
	PartnerName string           `json:"partner_name,omitempty"`
	Court       string           `json:"court,omitempty"`
	Note        string           `json:"note,omitempty"`
	Drills      []MatchPrepDrill `json:"drills,omitempty"`
}

// MatchPrepDrill mirrors the drill shape the prep API accepts on create — the
// client can append these to the new-prep form with no transform.
type MatchPrepDrill struct {
	Title           string `json:"title"`
	DurationSeconds int    `json:"duration_seconds"`
}

const matchPrepPrefix = "[MATCH_PREP:"

// ParseMatchPrepToken returns the LAST valid [MATCH_PREP: {...}] token in
// text. The "last" rule (not first) is deliberate: when Marco accidentally
// double-emits — usually because prior-turn context bled into the new reply
// — the user's CURRENT intent is the trailing token. Taking the last one
// keeps the client opening the right sheet even when the prompt rule slips.
// Returns nil if no valid token is found.
//
// Unlike [MATCH_LOG: ...], the prep token can contain a nested drills array
// with object items, so the inner JSON has more than one level of braces. We
// scan with a manual brace counter (skipping strings) rather than a regex —
// Go's RE2 has no recursion support.
func ParseMatchPrepToken(text string) *MatchPrepToken {
	var last *MatchPrepToken
	cursor := 0
	for {
		raw, next, ok := findMatchPrepPayloadFrom(text, cursor)
		if !ok {
			break
		}
		cursor = next
		var t MatchPrepToken
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			continue
		}
		if t.Mode != "adjust" && t.Mode != "create" {
			continue
		}
		last = &t
	}
	return last
}

// CleanMatchPrepTokens removes every [MATCH_PREP: ...] token from text. Used
// by CleanContent so the assistant message we ship to the chat UI carries no
// raw prompt artefacts.
func CleanMatchPrepTokens(text string) string {
	for {
		start := strings.Index(text, matchPrepPrefix)
		if start < 0 {
			break
		}
		end, ok := matchPrepClose(text, start)
		if !ok {
			break
		}
		text = text[:start] + text[end+1:]
	}
	return strings.TrimSpace(text)
}

// findMatchPrepPayloadFrom locates the next MATCH_PREP JSON payload at or
// after `from` in `text`. Returns the JSON string and the index immediately
// past the consumed token, so callers can resume scanning. ok=false when no
// balanced token is found.
func findMatchPrepPayloadFrom(text string, from int) (string, int, bool) {
	rel := strings.Index(text[from:], matchPrepPrefix)
	if rel < 0 {
		return "", 0, false
	}
	start := from + rel
	objStart := strings.Index(text[start:], "{")
	if objStart < 0 {
		return "", 0, false
	}
	objStart += start
	objEnd, ok := balancedBraces(text, objStart)
	if !ok {
		return "", 0, false
	}
	return text[objStart : objEnd+1], objEnd + 1, true
}

// matchPrepClose returns the index of the closing ']' for a token that begins
// at `start` (the index of '[').
func matchPrepClose(text string, start int) (int, bool) {
	objStart := strings.Index(text[start:], "{")
	if objStart < 0 {
		return 0, false
	}
	objStart += start
	objEnd, ok := balancedBraces(text, objStart)
	if !ok {
		return 0, false
	}
	closeBracket := strings.Index(text[objEnd:], "]")
	if closeBracket < 0 {
		return 0, false
	}
	return objEnd + closeBracket, true
}

// balancedBraces returns the index of the '}' that closes the '{' at `open`,
// respecting JSON string literals so braces inside strings do not throw off
// the depth count.
func balancedBraces(text string, open int) (int, bool) {
	if open >= len(text) || text[open] != '{' {
		return 0, false
	}
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
