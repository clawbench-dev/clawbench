package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// A proofread run chunks a long transcript and asks the agent once per chunk.
// The failure has to name WHICH chunk broke: "chunk 3" tells the user the run got
// that far and which part of the meeting is the problem, while a bare "agent
// revise failed" leaves them guessing.
func TestReviseLiveNotesTranscriptWithAgent_NamesTheFailingChunk(t *testing.T) {
	rec := newEndpointRecorder(t, "x")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	// Two chunks' worth of transcript, so the failure lands inside the loop
	// rather than on the final assembly.
	long := strings.Repeat("讨论配置加载器的实现。", liveNotesReviseChunkRunes)
	rec.status = http.StatusInternalServerError

	_, err := reviseLiveNotesTranscriptWithAgent(context.Background(), LiveNotesReviseInput{
		Segments: liveNoteSegmentsForAgent(long),
		Title:    "周会",
	}, t.TempDir())
	if err == nil {
		t.Fatal("a failing endpoint must be reported")
	}
	if !strings.Contains(err.Error(), "chunk 1") {
		t.Fatalf("the error must name the chunk, got %v", err)
	}
}

// An answer the parser cannot read is also attributed to its chunk: the model
// answered, but not in the `原词 => 改词` form the applier needs.
func TestReviseLiveNotesTranscriptWithAgent_RejectsAnUnparseableChunk(t *testing.T) {
	// A model that echoes the transcript back has no pairs to apply.
	rec := newEndpointRecorder(t, "这是原文的复述，没有箭头。")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	_, err := reviseLiveNotesTranscriptWithAgent(context.Background(), LiveNotesReviseInput{
		Segments: liveNoteSegmentsForAgent("讨论配置加载器的实现。"),
		Title:    "周会",
	}, t.TempDir())
	if err == nil {
		t.Fatal("an answer with no pairs must be refused")
	}
	if !strings.Contains(err.Error(), "chunk 1") {
		t.Fatalf("the parse failure must name the chunk, got %v", err)
	}
}

// An empty transcript is refused before any model call: there is nothing to
// proofread, and a round trip would only cost time.
func TestReviseLiveNotesTranscriptWithAgent_EmptyTranscriptSkipsTheModel(t *testing.T) {
	rec := newEndpointRecorder(t, "a => b")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	_, err := reviseLiveNotesTranscriptWithAgent(context.Background(), LiveNotesReviseInput{
		Segments: nil,
		Title:    "周会",
	}, t.TempDir())
	if err == nil {
		t.Fatal("an empty transcript must be refused")
	}
	select {
	case <-rec.bodies:
		t.Fatal("an empty transcript must not reach the model")
	default:
	}
}

// liveNoteSegmentsForAgent wraps one transcript string into the segment shape the
// revise path consumes.
func liveNoteSegmentsForAgent(text string) []LiveNoteSegment {
	return []LiveNoteSegment{{ID: 1, Text: text}}
}

// ListMeetingTokens feeds "clear all". It must include hidden rows (they have no
// other way to be reached) and be ordered newest first, so a partial clear makes a
// dent in the most recent meetings first.
func TestListMeetingTokens_IncludesHiddenRowsNewestFirst(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	pid, err := store.ProjectIDForPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherPID, err := store.ProjectIDForPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []struct {
		token string
		start int64
		pid   int64
	}{
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 1000, pid},
		{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 3000, pid},
		{"cccccccccccccccccccccccccccccccc", 2000, pid},
		{"dddddddddddddddddddddddddddddddd", 9999, otherPID},
	} {
		if err := StartLiveNote(m.token, m.pid, "会", m.start); err != nil {
			t.Fatalf("StartLiveNote: %v", err)
		}
	}
	// A hidden row (a pre-record meeting the upgrade could not place on a
	// timeline): it never shows in the list view, but clear-all must see it.
	if _, err := store.WriteExec(`UPDATE live_notes SET hidden = 1 WHERE token = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`); err != nil {
		t.Fatalf("hide row: %v", err)
	}

	got, err := ListMeetingTokens(pid)
	if err != nil {
		t.Fatalf("ListMeetingTokens: %v", err)
	}
	want := []string{
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tokens = %v, want %v (newest first, hidden included, other project excluded)", got, want)
	}

	// A project with no meetings is an empty list, not an error.
	empty, err := ListMeetingTokens(pid + 1000)
	if err != nil {
		t.Fatalf("ListMeetingTokens(empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no tokens, got %v", empty)
	}
}

// A closed database is what the panel hits during a restart; the list call must
// report it instead of returning an empty list that reads as "no meetings".
func TestListMeetingTokens_ReportsAClosedDatabase(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ListMeetingTokens(1); err == nil {
		t.Fatal("a closed database must be an error")
	}
}
