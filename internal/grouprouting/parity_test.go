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
type corpus struct {
	Cases []struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Want struct {
			Found       bool     `json:"found"`
			End         bool     `json:"end"`
			Speakers    []string `json:"speakers"`
			Instruction string   `json:"instruction"`
			Before      string   `json:"before"`
		} `json:"want"`
	} `json:"cases"`
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
			if r.Before != tc.Want.Before {
				t.Errorf("before=%q want %q", r.Before, tc.Want.Before)
			}
		})
	}
}
