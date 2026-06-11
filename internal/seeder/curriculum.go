// Package seeder parses the marco_curriculum_v2.md source file into typed
// records and writes them into the database. The markdown file is the single
// source of truth for lesson content — this package does not invent or
// paraphrase any field values.
package seeder

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Cue struct {
	TimestampSeconds int
	CueText          string
}

type Mistake struct {
	Pct  int
	Text string
}

type Drill struct {
	Name            string
	DurationMinutes int
	IsRecommended   bool
	Description     string
}

type Lesson struct {
	Number  int
	Level   string
	Title   string
	Tagline string
	Focus   string
	Cues    []Cue
	Mistake Mistake
	Drill   Drill
}

// Recognised levels — lowercase, matching the DB CHECK constraint.
var levelMap = map[string]string{
	"BEGINNER":     "beginner",
	"INTERMEDIATE": "intermediate",
	"ADVANCED":     "advanced",
}

var (
	// **LESSON 1 · BEGINNER**
	headerRE = regexp.MustCompile(`^\*\*LESSON\s+(\d+)\s+·\s+(BEGINNER|INTERMEDIATE|ADVANCED)\*\*\s*$`)
	// Indented cue: "  0:03 — V between thumb and index finger sits on the top bevel"
	// Allow em-dash, en-dash, or hyphen.
	cueRE = regexp.MustCompile(`^\s+0:(\d+)\s+[—–-]\s+(.+)$`)
	// Common Mistake (62% of beginners): ...
	mistakeRE = regexp.MustCompile(`^Common Mistake\s*\((\d+)%[^)]*\):\s*(.+)$`)
	// Drill: Grip Freeze · 5 min · Recommended · After every shot, ...
	drillRE = regexp.MustCompile(`^Drill:\s+(.+?)\s+·\s+(\d+)\s*min\s+·\s+(Recommended|Optional)\s+·\s+(.+)$`)
)

// ParseFile reads the curriculum markdown at path and returns its lessons.
func ParseFile(path string) ([]Lesson, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open curriculum: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads the curriculum markdown from r and returns its lessons.
func Parse(r io.Reader) ([]Lesson, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		lessons []Lesson
		current *Lesson
	)
	flush := func() error {
		if current == nil {
			return nil
		}
		if err := validateLesson(current); err != nil {
			return err
		}
		lessons = append(lessons, *current)
		current = nil
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		if m := headerRE.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return nil, err
			}
			n, _ := strconv.Atoi(m[1])
			current = &Lesson{Number: n, Level: levelMap[m[2]]}
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "Title:"):
			current.Title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))

		case strings.HasPrefix(line, "Tagline:"):
			current.Tagline = stripQuotes(strings.TrimSpace(strings.TrimPrefix(line, "Tagline:")))

		case strings.HasPrefix(line, "Focus:"):
			current.Focus = strings.TrimSpace(strings.TrimPrefix(line, "Focus:"))

		case strings.HasPrefix(line, "Watch For:"):
			// Cue lines follow on subsequent iterations.

		case cueRE.MatchString(line):
			m := cueRE.FindStringSubmatch(line)
			secs, _ := strconv.Atoi(m[1])
			current.Cues = append(current.Cues, Cue{
				TimestampSeconds: secs,
				CueText:          strings.TrimSpace(m[2]),
			})

		case mistakeRE.MatchString(line):
			m := mistakeRE.FindStringSubmatch(line)
			pct, _ := strconv.Atoi(m[1])
			current.Mistake = Mistake{Pct: pct, Text: strings.TrimSpace(m[2])}

		case strings.HasPrefix(line, "Drill:"):
			m := drillRE.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("lesson %d: drill line did not match expected format: %q", current.Number, line)
			}
			dur, _ := strconv.Atoi(m[2])
			current.Drill = Drill{
				Name:            strings.TrimSpace(m[1]),
				DurationMinutes: dur,
				IsRecommended:   m[3] == "Recommended",
				Description:     strings.TrimSpace(m[4]),
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan curriculum: %w", err)
	}
	if err := flush(); err != nil {
		return nil, err
	}

	if len(lessons) == 0 {
		return nil, fmt.Errorf("no lessons parsed — check curriculum file format")
	}
	return lessons, nil
}

func validateLesson(l *Lesson) error {
	if l.Title == "" {
		return fmt.Errorf("lesson %d: missing title", l.Number)
	}
	if l.Tagline == "" {
		return fmt.Errorf("lesson %d (%q): missing tagline", l.Number, l.Title)
	}
	if l.Focus == "" {
		return fmt.Errorf("lesson %d (%q): missing focus", l.Number, l.Title)
	}
	if len(l.Cues) != 3 {
		return fmt.Errorf("lesson %d (%q): expected 3 cues, got %d", l.Number, l.Title, len(l.Cues))
	}
	if l.Mistake.Text == "" {
		return fmt.Errorf("lesson %d (%q): missing common mistake", l.Number, l.Title)
	}
	if l.Drill.Name == "" || l.Drill.Description == "" || l.Drill.DurationMinutes == 0 {
		return fmt.Errorf("lesson %d (%q): incomplete drill", l.Number, l.Title)
	}
	if l.Level == "" {
		return fmt.Errorf("lesson %d: unknown level", l.Number)
	}
	return nil
}

// stripQuotes removes a surrounding pair of straight or smart quotes.
func stripQuotes(s string) string {
	openers := []string{`"`, "“", "‘", "«"}
	closers := []string{`"`, "”", "’", "»"}
	for i, o := range openers {
		c := closers[i]
		if strings.HasPrefix(s, o) && strings.HasSuffix(s, c) {
			return s[len(o) : len(s)-len(c)]
		}
	}
	return s
}

// Slugify produces a kebab-case slug from a lesson title. Strips Spanish/French
// accents so e.g. "The Víbora" -> "the-vibora" and "Chassé Footwork" ->
// "chasse-footwork".
func Slugify(s string) string {
	replacer := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u",
		"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u",
		"ñ", "n", "Ñ", "n",
		"ü", "u", "Ü", "u",
		"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
	)
	s = replacer.Replace(strings.ToLower(s))

	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
