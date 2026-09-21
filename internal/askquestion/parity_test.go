package askquestion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// corpus is the shared Go/TS fixture. The TypeScript mirror
// (web/src/utils/__tests__/askQuestionParity.test.ts) reads the same file, so
// both implementations are pinned to identical results.
type corpus struct {
	Input []struct {
		Name string         `json:"name"`
		Raw  map[string]any `json:"raw"`
		Want []Item         `json:"want"`
	} `json:"input"`
	Extract []struct {
		Name         string   `json:"name"`
		Text         string   `json:"text"`
		WantParsed   []bool   `json:"wantParsed"`
		WantItems    [][]Item `json:"wantItems"`
		WantStripped string   `json:"wantStripped"`
		// WantFallbacks pins the text an unparsed span degrades to. It is what
		// a legacy span renders as, so a regression there is a user-visible
		// defect (leaked `false`, a lost separator) rather than a test detail.
		WantFallbacks []string `json:"wantFallbacks"`
		// WantReasons pins the reason code of every span, so a legacy span
		// cannot silently become a parse failure (or vice versa).
		WantReasons []string `json:"wantReasons"`
	} `json:"extract"`
}

func loadCorpus(t *testing.T) corpus {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "parity_corpus.json"))
	if err != nil {
		t.Fatalf("read parity corpus: %v", err)
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("parse parity corpus: %v", err)
	}
	if len(c.Input) == 0 || len(c.Extract) == 0 {
		t.Fatal("parity corpus must not be empty")
	}
	return c
}

func TestParityCorpus_NormalizeInput(t *testing.T) {
	c := loadCorpus(t)
	for _, tc := range c.Input {
		t.Run(tc.Name, func(t *testing.T) {
			got := NormalizeInput(tc.Raw)
			// A nil result and an empty slice are the same outcome; the JSON
			// fixture can only express it as an empty array.
			if len(got) == 0 && len(tc.Want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.Want) {
				t.Errorf("NormalizeInput mismatch\n got: %+v\nwant: %+v", got, tc.Want)
			}
		})
	}
}

func TestParityCorpus_Extract(t *testing.T) {
	c := loadCorpus(t)
	for _, tc := range c.Extract {
		t.Run(tc.Name, func(t *testing.T) {
			ms := Extract(tc.Text)
			if len(ms) != len(tc.WantParsed) {
				t.Fatalf("expected %d matches, got %d (%+v)", len(tc.WantParsed), len(ms), ms)
			}
			for i, wantParsed := range tc.WantParsed {
				if ms[i].Parsed != wantParsed {
					t.Errorf("match %d: parsed=%v want %v (reason %q)",
						i, ms[i].Parsed, wantParsed, ms[i].Reason)
				}
				if !wantParsed {
					continue
				}
				if !reflect.DeepEqual(ms[i].Items, tc.WantItems[i]) {
					t.Errorf("match %d items mismatch\n got: %+v\nwant: %+v",
						i, ms[i].Items, tc.WantItems[i])
				}
			}
			for i := range tc.WantFallbacks {
				if ms[i].Fallback != tc.WantFallbacks[i] {
					t.Errorf("match %d fallback mismatch\n got: %q\nwant: %q",
						i, ms[i].Fallback, tc.WantFallbacks[i])
				}
			}
			for i := range tc.WantReasons {
				if ms[i].Reason != tc.WantReasons[i] {
					t.Errorf("match %d reason = %q, want %q", i, ms[i].Reason, tc.WantReasons[i])
				}
			}
			if got := Strip(tc.Text, ms); got != tc.WantStripped {
				t.Errorf("stripped mismatch\n got: %q\nwant: %q", got, tc.WantStripped)
			}
		})
	}
}
