package grouprouting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// corpus is the shared Go/TS fixture. The TypeScript mirror
// (web/src/utils/__tests__/groupRoutingParity.test.ts) reads the same file, so
// both implementations are pinned to identical results.
//
// It has TWO case sets:
//   - Cases: Parse() results.
//   - StripCases: StripProtocolTags() results. These pin the SECURITY-CRITICAL
//     fail-closed contract (a private note must never survive, through any
//     shape) across both implementations. Without them the two strip functions
//     diverged silently on an unclosed private note (a real cross-member leak).
type corpus struct {
	Cases []struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Want struct {
			Found       bool           `json:"found"`
			End         bool           `json:"end"`
			Speakers    []string       `json:"speakers"`
			Groups      []MentionGroup `json:"groups"`
			Instruction string         `json:"instruction"`
			Before      string         `json:"before"`
			After       string         `json:"after"`
			Bcc         []BccEntry     `json:"bcc"`
		} `json:"want"`
	} `json:"cases"`
	StripCases []struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Want string `json:"want"`
	} `json:"stripCases"`
}

func TestParityCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "parity_corpus.json"))
	if err != nil {
		t.Fatalf("read parity corpus: %v", err)
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("parse parity corpus: %v", err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("parity corpus must not be empty")
	}
	for _, tc := range c.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			r := Parse(tc.Text)
			if r.Found != tc.Want.Found {
				t.Errorf("found=%v want %v", r.Found, tc.Want.Found)
			}
			if r.End != tc.Want.End {
				t.Errorf("end=%v want %v", r.End, tc.Want.End)
			}
			got := r.Speakers
			if got == nil {
				got = []string{}
			}
			want := tc.Want.Speakers
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("speakers=%v want %v", got, want)
			}
			if r.Instruction != tc.Want.Instruction {
				t.Errorf("instruction=%q want %q", r.Instruction, tc.Want.Instruction)
			}
			gotGroups := r.Groups
			if gotGroups == nil {
				gotGroups = []MentionGroup{}
			}
			wantGroups := tc.Want.Groups
			if wantGroups == nil {
				wantGroups = []MentionGroup{}
			}
			if !reflect.DeepEqual(gotGroups, wantGroups) {
				t.Errorf("groups=%v want %v", gotGroups, wantGroups)
			}
			if r.Before != tc.Want.Before {
				t.Errorf("before=%q want %q", r.Before, tc.Want.Before)
			}
			if r.After != tc.Want.After {
				t.Errorf("after=%q want %q", r.After, tc.Want.After)
			}
			gotBcc := r.Bcc
			if gotBcc == nil {
				gotBcc = []BccEntry{}
			}
			wantBcc := tc.Want.Bcc
			if wantBcc == nil {
				wantBcc = []BccEntry{}
			}
			if !reflect.DeepEqual(gotBcc, wantBcc) {
				t.Errorf("bcc=%v want %v", gotBcc, wantBcc)
			}
		})
	}
}

// TestParityCorpusStrip pins StripProtocolTags (the fail-closed injection /
// quote / TTS / push boundary) across Go and TS. A private note must never
// survive, in ANY shape — well-formed, malformed, nested, or unclosed.
func TestParityCorpusStrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "parity_corpus.json"))
	if err != nil {
		t.Fatalf("read parity corpus: %v", err)
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("parse parity corpus: %v", err)
	}
	if len(c.StripCases) == 0 {
		t.Fatal("parity corpus must have stripCases (the fail-closed contract is security-critical)")
	}
	for _, tc := range c.StripCases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := StripProtocolTags(tc.Text); got != tc.Want {
				t.Errorf("StripProtocolTags(%q) = %q, want %q", tc.Text, got, tc.Want)
			}
		})
	}
}
