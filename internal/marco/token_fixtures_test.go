package marco

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// token_fixtures.json is the shared contract for Marco's inline token grammar.
// An IDENTICAL copy lives in the marco-app repo
// (src/components/chat/token_fixtures.json), where Jest runs the TypeScript
// strippers/parsers against the same cases. If a change here makes this test
// disagree with the fixtures, update the grammar in prompt.md, the fixtures,
// and BOTH repos' implementations together.
type tokenFixtureFile struct {
	Cases []tokenFixtureCase `json:"cases"`
}

type tokenFixtureCase struct {
	Name       string         `json:"name"`
	Input      string         `json:"input"`
	CleanText  string         `json:"clean_text"`
	LessonRefs []LessonRef    `json:"lesson_refs"`
	MatchLog   *MatchLogToken `json:"match_log"`
	MatchPrep  *struct {
		Mode string `json:"mode"`
		ID   string `json:"id"`
	} `json:"match_prep"`
}

func TestTokenGrammarFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "token_fixtures.json"))
	require.NoError(t, err)

	var f tokenFixtureFile
	require.NoError(t, json.Unmarshal(raw, &f))
	require.NotEmpty(t, f.Cases)

	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			assert.Equal(t, tc.CleanText, CleanContent(tc.Input), "CleanContent")
			assert.Equal(t, tc.LessonRefs, ParseLessonRefs(tc.Input), "ParseLessonRefs")
			assert.Equal(t, tc.MatchLog, ParseMatchLogToken(tc.Input), "ParseMatchLogToken")

			prep := ParseMatchPrepToken(tc.Input)
			if tc.MatchPrep == nil {
				assert.Nil(t, prep, "ParseMatchPrepToken: expected no valid token")
			} else {
				require.NotNil(t, prep, "ParseMatchPrepToken: expected a valid token")
				assert.Equal(t, tc.MatchPrep.Mode, prep.Mode, "match prep mode")
				assert.Equal(t, tc.MatchPrep.ID, prep.ID, "match prep id")
			}
		})
	}
}
