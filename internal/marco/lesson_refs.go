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

// Two or more consecutive blank lines, i.e. what a stripped token leaves when
// it was written on its own line. Mirrored in marco-app (marcoTokens.ts).
var blankLineRunRegex = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)

// CleanContent removes all structured tokens ([LESSON_REF:...], [MATCH_LOG:...],
// [MATCH_PREP:...]) from text so stored assistant messages can be returned to
// the client without raw prompt artefacts.
func CleanContent(text string) string {
	// Substitute the title, don't delete it. Marco is told to write the token
	// inline ("the next step is [LESSON_REF: ...]"), so removing it outright
	// left the sentence without its subject — on screen that read "I'd begin
	// with the foundations of net play: . That gives you the core mechanics".
	// The tappable card is rendered separately from ParseLessonRefs, so the
	// title appearing in the prose is the mention, not a duplicate of the card.
	text = lessonRefRegex.ReplaceAllString(text, "$2")
	text = matchLogRegex.ReplaceAllString(text, "")
	text = CleanMatchPrepTokens(text)
	// A token that sat on a line of its own leaves the blank lines that framed
	// it behind, and the chat bubble renders that as a hole in the middle of
	// Marco's reply. Collapse any run of blank lines back to a single one.
	// Interior horizontal runs stay as they are — the fixtures pin that.
	text = blankLineRunRegex.ReplaceAllString(text, "\n\n")
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
