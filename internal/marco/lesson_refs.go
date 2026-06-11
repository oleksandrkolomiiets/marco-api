package marco

import (
	"regexp"
	"strings"
)

type LessonRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Allowed id characters: letters, digits, underscore, hyphen. The seeded
// lesson slugs use hyphens (ready-position, forehand-drive), so the parser
// must accept them for the prompt's LESSON_REF format to round-trip end-to-end.
var lessonRefRegex = regexp.MustCompile(`\[LESSON_REF:\s*([a-zA-Z0-9_-]+)\s*\|\s*"([^"]+)"\s*\]`)

// CleanContent removes all structured tokens ([LESSON_REF:...], [MATCH_LOG:...],
// [MATCH_PREP:...]) from text so stored assistant messages can be returned to
// the client without raw prompt artefacts.
func CleanContent(text string) string {
	text = lessonRefRegex.ReplaceAllString(text, "")
	text = matchLogRegex.ReplaceAllString(text, "")
	text = CleanMatchPrepTokens(text)
	return strings.TrimSpace(text)
}

// ParseLessonRefs extracts all [LESSON_REF: id | "title"] tokens from text.
// Returns an empty slice (not nil) if no refs are found, so the JSON serialisation
// produces [] not null.
func ParseLessonRefs(text string) []LessonRef {
	refs := []LessonRef{}
	matches := lessonRefRegex.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		refs = append(refs, LessonRef{ID: m[1], Title: m[2]})
	}
	return refs
}
