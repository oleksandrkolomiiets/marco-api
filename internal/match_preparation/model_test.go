package match_preparation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputePreparationPct(t *testing.T) {
	cases := []struct {
		name   string
		drills []Drill
		want   int
	}{
		{name: "empty queue is zero", drills: nil, want: 0},
		{name: "all done is 100", drills: []Drill{{Completed: true}, {Completed: true}}, want: 100},
		{name: "none done is 0", drills: []Drill{{Completed: false}, {Completed: false}}, want: 0},
		{name: "two of three rounds to 67", drills: []Drill{{Completed: true}, {Completed: true}, {Completed: false}}, want: 67},
		{name: "one of three rounds to 33", drills: []Drill{{Completed: true}, {Completed: false}, {Completed: false}}, want: 33},
		{name: "one of two rounds to 50", drills: []Drill{{Completed: true}, {Completed: false}}, want: 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, computePreparationPct(tc.drills))
		})
	}
}
