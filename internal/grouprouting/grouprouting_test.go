package grouprouting

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSingleSpeaker(t *testing.T) {
	r := Parse(`<clawbench-speaker>B</clawbench-speaker> 请回应 A 的质疑`)
	if !r.Found {
		t.Fatal("expected Found=true")
	}
	if r.End {
		t.Fatal("expected End=false")
	}
	if !reflect.DeepEqual(r.Speakers, []string{"B"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
	if r.Instruction != "请回应 A 的质疑" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}

func TestParseMultipleSpeakers(t *testing.T) {
	r := Parse(`<clawbench-speaker>A, B ,C</clawbench-speaker> 各自表态`)
	if !reflect.DeepEqual(r.Speakers, []string{"A", "B", "C"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
}

func TestParseEndSignal(t *testing.T) {
	r := Parse(`讨论已充分。<clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true")
	}
}

// TestParseBothTags: a message may carry a speaker tag AND an end tag. End must
// win (the orchestrator breaks the loop), and the end tag must not leak into the
// instruction.
func TestParseBothTags(t *testing.T) {
	r := Parse(`<clawbench-speaker>B</clawbench-speaker> 请回应<clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true (end wins)")
	}
	if strings.Contains(r.Instruction, "clawbench-group-end") {
		t.Fatalf("end tag leaked into instruction: %q", r.Instruction)
	}
	if r.Instruction != "请回应" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}

func TestParseMalformedKeepsRaw(t *testing.T) {
	in := `<clawbench-speaker></clawbench-speaker>`
	r := Parse(in)
	if r.Found {
		t.Fatal("empty payload must not be Found")
	}
	if r.Raw != in {
		t.Fatalf("Raw must equal source, got %q", r.Raw)
	}
}

func TestParseNoTag(t *testing.T) {
	r := Parse("普通发言，没有标签")
	if r.Found || r.End {
		t.Fatal("expected no tag")
	}
}
