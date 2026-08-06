package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupLetters() []string {
	seen := map[string]bool{}
	for _, c := range Cases {
		seen[c.ID[:1]] = true
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// `make qa-group GROUP=X` expanded to --filter X1,X2,X3,X4, which assumed
// every group holds exactly four cases. Only three of nine did, so the target
// died with "unknown case ids" for the rest — including GROUP=D, the example
// in the docs. Running every real group here means a group that gains or
// loses a case cannot quietly break the target again.
func TestGroupCases_EveryGroupSelectable(t *testing.T) {
	letters := groupLetters()
	require.NotEmpty(t, letters)

	total := 0
	for _, g := range letters {
		t.Run(g, func(t *testing.T) {
			got, err := groupCases(Cases, g)
			require.NoError(t, err)
			assert.NotEmpty(t, got)
			for _, c := range got {
				assert.True(t, strings.HasPrefix(c.ID, g),
					"case %s selected for group %s", c.ID, g)
			}
		})
		selected, err := groupCases(Cases, g)
		require.NoError(t, err)
		total += len(selected)
	}

	// Every case belongs to exactly one group, so the groups must partition
	// the suite — no case unreachable via qa-group.
	assert.Equal(t, len(Cases), total)
}

func TestGroupCases_RejectsAGroupWithNoCases(t *testing.T) {
	_, err := groupCases(Cases, "Z")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no cases in group "Z"`)
}

func TestGroupCases_EmptyGroupReturnsEverything(t *testing.T) {
	got, err := groupCases(Cases, "")
	require.NoError(t, err)
	assert.Len(t, got, len(Cases))
}

func TestGroupCases_IsCaseInsensitiveAndTrims(t *testing.T) {
	upper, err := groupCases(Cases, "E")
	require.NoError(t, err)
	lower, err := groupCases(Cases, "  e ")
	require.NoError(t, err)
	assert.Equal(t, upper, lower)
}

// --group narrows the pool and --filter narrows it again, so the two compose
// rather than fighting: `--group E --filter E1` is a legal way to say "just
// E1", and asking for a case outside the group is an error, not silence.
func TestGroupCases_ComposesWithFilter(t *testing.T) {
	pool, err := groupCases(Cases, "E")
	require.NoError(t, err)

	got, err := filterCases(pool, "E1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "E1", got[0].ID)

	_, err = filterCases(pool, "A1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown case ids")
}

func TestFilterCases_UnknownIDIsAnError(t *testing.T) {
	_, err := filterCases(Cases, "E99")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "E99")
}

func TestFilterCases_EmptyReturnsEverything(t *testing.T) {
	got, err := filterCases(Cases, "")
	require.NoError(t, err)
	assert.Len(t, got, len(Cases))
}

// The runner warns when a follow-up case runs without the case it continues.
// Every FollowsUp must name a real case, or that warning can never fire.
func TestCases_FollowUpsReferenceRealCases(t *testing.T) {
	ids := map[string]bool{}
	for _, c := range Cases {
		ids[c.ID] = true
	}
	for _, c := range Cases {
		if c.FollowsUp != "" {
			assert.True(t, ids[c.FollowsUp],
				"case %s follows up %s, which does not exist", c.ID, c.FollowsUp)
		}
	}
}

func TestCases_IDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Cases {
		assert.False(t, seen[c.ID], "duplicate case id %s", c.ID)
		seen[c.ID] = true
	}
}
