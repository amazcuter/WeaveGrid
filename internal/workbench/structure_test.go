package workbench

import (
	"context"
	"testing"
)

func assertGeneratedArcBoundaries(t *testing.T, s Structure) {
	t.Helper()
	incoming := make([]int, s.Chapters+1)
	for _, thread := range s.Threads {
		incoming[thread.Payoff]++
	}
	if incoming[s.Chapters] < 5 {
		t.Fatalf("%d chapters, seed %d: final chapter has only %d incoming threads", s.Chapters, s.Seed, incoming[s.Chapters])
	}
	start, arc := 1, 0
	for ch := 1; ch <= s.Chapters; ch++ {
		if incoming[ch] <= 4 {
			continue
		}
		want := [2]int{start, ch}
		if arc >= len(s.Arcs) || s.Arcs[arc] != want {
			t.Fatalf("%d chapters, seed %d: expected arc %v at index %d, got %v", s.Chapters, s.Seed, want, arc, s.Arcs)
		}
		found := false
		for _, node := range s.Nodes {
			if node.Ch == ch {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("arc ending at chapter %d has no turning node", ch)
		}
		start, arc = ch+1, arc+1
	}
	if arc != len(s.Arcs) || start != s.Chapters+1 {
		t.Fatalf("arcs must cover all chapters exactly once: %v", s.Arcs)
	}
	seen := map[int]bool{}
	for _, node := range s.Nodes {
		if seen[node.Ch] {
			t.Fatalf("duplicate turning node at chapter %d", node.Ch)
		}
		seen[node.Ch] = true
		if node.Arc < 1 || node.Arc > len(s.Arcs) {
			t.Fatalf("turning node has invalid arc: %+v", node)
		}
		a := s.Arcs[node.Arc-1]
		if node.Ch < a[0] || node.Ch > a[1] {
			t.Fatalf("turning node %+v is outside its arc %v", node, a)
		}
	}
}

func TestGeneratorFinalChapterClosesShortAndSparseStructures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chapters int
		seed     int64
	}{
		{"single_source", 3, 7},
		{"four_available_sources", 6, 7},
		{"five_available_sources", 7, 7},
		{"four_incoming_threads", 50, 19},
		{"no_incoming_threads", 100, 1},
		{"already_sufficient", 50, 7},
		{"consecutive_boundaries", 50, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := GenerateStructure(context.Background(), tc.chapters, tc.seed)
			if err != nil {
				t.Fatal(err)
			}
			assertGeneratedArcBoundaries(t, s)
			finalIncoming := 0
			for _, thread := range s.Threads {
				if thread.Payoff == tc.chapters {
					finalIncoming++
				}
			}
			if finalIncoming != 5 {
				t.Fatalf("only fill the deficit to five: got %d final incoming threads", finalIncoming)
			}
			if tc.name == "consecutive_boundaries" && s.Arcs[len(s.Arcs)-1] != [2]int{50, 50} {
				t.Fatalf("consecutive boundary chapters must retain a one-chapter unit: %v", s.Arcs)
			}
		})
	}
}
