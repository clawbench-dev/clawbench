package service

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clawbench/internal/store"
)

// This file builds the prompt blocks that tell the two model stages what was
// going on around the transcript. They are separate from live_notes.go because
// they share one idea — "the transcript is not the whole meeting" — and because
// each one is a small, independently testable renderer.

// ── Glossary ────────────────────────────────────────────────────────────────

// liveNoteGlossaryMaxTerms / Runes bound the vocabulary block. The proofreader
// gets it on every chunk, so it competes with the transcript for the window.
const (
	liveNoteGlossaryMaxTerms = 80
	liveNoteGlossaryMaxRunes = 1200
)

// liveNotePlaceholderLabel matches speaker labels that carry no name: an
// ordinal assigned by the client to a viewer who never typed one.
var liveNotePlaceholderLabel = regexp.MustCompile(`^(?:观众|参会者|成员|viewer|member|guest|participant)\s*\d+$`)

// liveNoteDocIdentifier matches a candidate identifier inside a project
// document. Prose words are filtered out afterwards by liveNoteIsTermLike.
var liveNoteDocIdentifier = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_./-]{1,39}`)

// liveNoteBacktickSpan matches a code span in a project document.
var liveNoteBacktickSpan = regexp.MustCompile("`([^`\n]{1,60})`")

// liveNoteDocHeader matches the `--- README.md ---` banner projectContext puts
// in front of the document. It is our own framing, not the document's text, and
// the file name it carries is already covered by the file timeline.
var liveNoteDocHeader = regexp.MustCompile(`(?m)^--- .+ ---$`)

// LiveNoteGlossaryPromptSection renders the meeting's own vocabulary: the
// title, the participants, the documents people opened, and the identifiers the
// project's own documents spell correctly.
//
// This is what the proofreader needs and the minutes do not: it is a spelling
// reference, not content. The project documents were already injected, but only
// the FIRST one and only as prose — while the words that actually get misheard
// are identifiers (ClawBench, useLiveNotes.ts, ACP), which are a small fraction
// of a 4 KiB README and are exactly what a regex can lift out of it.
//
// Order is priority: a name the meeting used is worth more than a name some
// document happens to contain.
func LiveNoteGlossaryPromptSection(title string, files []LiveNoteFile, segments []LiveNoteSegment, projectPath string) string {
	seen := make(map[string]bool, 32)
	terms := make([]string, 0, 32)
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" || seen[term] || utf8.RuneCountInString(term) > 40 {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}

	// A placeholder title is a timestamp, not a name: handing it over as a
	// domain term invites the model to "correct" numbers.
	if t := strings.TrimSpace(title); t != "" && !IsPlaceholderMeetingTitle(t) {
		add(t)
	}
	for _, label := range liveNoteParticipantLabels(segments) {
		add(label)
	}
	for _, f := range files {
		add(liveNoteBaseName(f.Path))
		add(liveNoteBaseName(f.Name))
	}
	for _, term := range liveNoteDocTerms(strings.Join(projectContext(projectPath), "\n")) {
		add(term)
	}
	if len(terms) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 术语表（本次会议相关的正确写法）\n")
	b.WriteString("以下是本次会议出现的专有名词。转写里与它们接近的写法，按这里的拼写校正：\n\n")
	budget := liveNoteGlossaryMaxRunes
	for i, term := range terms {
		if i >= liveNoteGlossaryMaxTerms || budget <= 0 {
			break
		}
		b.WriteString("- ")
		b.WriteString(term)
		b.WriteString("\n")
		budget -= utf8.RuneCountInString(term) + 3
	}
	return strings.TrimRight(b.String(), "\n")
}

// liveNoteDocTerms lifts the identifiers out of a project document.
//
// Only code spans and code-shaped tokens are kept. A README is mostly prose, and
// handing the whole thing over as a "glossary" buries the twenty words that
// matter under a thousand that do not.
func liveNoteDocTerms(doc string) []string {
	if doc == "" {
		return nil
	}
	doc = liveNoteDocHeader.ReplaceAllString(doc, "")
	out := make([]string, 0, 32)
	for _, m := range liveNoteBacktickSpan.FindAllStringSubmatch(doc, -1) {
		if liveNoteLooksLikeTerm(m[1]) {
			out = append(out, m[1])
		}
	}
	for _, token := range liveNoteDocIdentifier.FindAllString(doc, -1) {
		// The regex happily eats the full stop that ends a sentence, which
		// would turn every "small." into an identifier. Trailing punctuation is
		// never part of a name.
		if liveNoteIsTermLike(strings.TrimRight(token, ".-/:")) {
			out = append(out, strings.TrimRight(token, ".-/:"))
		}
	}
	return out
}

// liveNoteLooksLikeTerm rejects anything that is not a single bare token: a
// command line, a URL, a path into another machine.
func liveNoteLooksLikeTerm(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > 40 {
		return false
	}
	if strings.Contains(s, "://") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "/") {
		return false
	}
	for _, r := range s {
		if r < utf8.RuneSelf {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-./:", r) {
				continue
			}
			return false
		}
		// Non-ASCII is allowed only when the token is otherwise a name; a
		// Chinese sentence fragment inside backticks is prose, not a term.
		return false
	}
	return true
}

// liveNoteIsTermLike reports whether a bare token looks like code rather than an
// ordinary word: an acronym, a CamelCase name, or something with a separator or
// a digit in it.
//
// Without this, every capitalised word at the start of a sentence ("The", "We")
// would enter the glossary.
func liveNoteIsTermLike(token string) bool {
	if !liveNoteLooksLikeTerm(token) {
		return false
	}
	letters := 0
	upper := 0
	for i, r := range token {
		switch {
		case r >= 'a' && r <= 'z':
			letters++
		case r >= 'A' && r <= 'Z':
			letters++
			upper++
			if i > 0 {
				return true // CamelCase
			}
		case r == '_' || r == '.' || r == '/' || r == '-' || (r >= '0' && r <= '9'):
			return true // identifier or version
		}
	}
	// All-caps acronym, e.g. ACP / API / README.
	return letters >= 2 && upper == letters
}

// liveNoteParticipantLabels returns the speaker labels in first-seen order,
// without the ones that carry no name.
func liveNoteParticipantLabels(segments []LiveNoteSegment) []string {
	seen := make(map[string]bool, 8)
	out := make([]string, 0, 8)
	for _, s := range segments {
		label := strings.TrimSpace(s.Speaker)
		if label == "" {
			label = strings.TrimSpace(s.Channel)
		}
		if !liveNoteLabelHasName(label) || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	return out
}

// liveNoteLabelHasName reports whether a speaker label is a person's name rather
// than a pronoun or an assigned ordinal.
func liveNoteLabelHasName(label string) bool {
	l := strings.TrimSpace(label)
	if l == "" {
		return false
	}
	switch strings.ToLower(l) {
	case "我", "me", "i", "myself", "自己":
		return false
	}
	return !liveNotePlaceholderLabel.MatchString(strings.ToLower(l))
}

// liveNoteBaseName returns the last path element, or "" for an empty path.
func liveNoteBaseName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(strings.ReplaceAll(path, "\\", "/"))
}

// ── Meeting info ────────────────────────────────────────────────────────────

// liveNoteMaxParticipants bounds the roster line. A 300-person webinar would
// otherwise be the whole block.
const liveNoteMaxParticipants = 12

// LiveNoteMeetingInfo is the framing the minutes need and the transcript cannot
// carry: when the meeting ran, how long, who was in it.
//
// Without it the model has no idea whether it is summarizing nine minutes or
// three hours, cannot write "本次会议 47 分钟", and has no way to tell whether a
// name mentioned as an owner was even present.
type LiveNoteMeetingInfo struct {
	ProjectName string
	// ProjectPath is the root the file section reads excerpts from. It is not
	// rendered — it is the path, not a fact about the meeting.
	ProjectPath string
	StartedAt   int64
	EndedAt     int64
	Segments    []LiveNoteSegment
	Screens     []LiveNoteScreenShare
	Self        LiveNotesSpeaker
}

// PromptSection renders the block, or "" when there is nothing factual to say.
func (i LiveNoteMeetingInfo) PromptSection() string {
	var rows []string
	if i.StartedAt > 0 {
		start := time.UnixMilli(i.StartedAt)
		span := start.Format("2006-01-02 15:04")
		switch {
		case i.EndedAt > i.StartedAt:
			span += "–" + time.UnixMilli(i.EndedAt).Format("15:04")
			if d := liveNoteDurationValue(i.EndedAt - i.StartedAt); d != "" {
				span += "（共 " + d + "）"
			}
		default:
			span += " 起（记录时会议仍在进行）"
		}
		rows = append(rows, "- 时间："+span)
	}
	if name := strings.TrimSpace(i.ProjectName); name != "" {
		rows = append(rows, "- 项目："+name)
	}
	if roster := i.roster(); roster != "" {
		rows = append(rows, "- 参会人："+roster)
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 会议信息\n")
	for _, row := range rows {
		b.WriteString(row)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// liveNoteMember is one person in the roster.
type liveNoteMember struct {
	name    string
	lines   int
	screen  bool
	firstAt int
}

// roster lists the people the meeting actually heard from, busiest first.
//
// Screen sharers are added even with no speech: a meeting where someone presented
// silently still had that person in it.
func (i LiveNoteMeetingInfo) roster() string {
	members := i.rosterMembers()
	if len(members) == 0 {
		return ""
	}
	selfLabel := strings.TrimSpace(i.Self.Label)
	parts := make([]string, 0, len(members))
	for _, m := range members {
		parts = append(parts, m.label(selfLabel))
	}
	return strings.Join(parts, "、")
}

// rosterMembers collects, orders and caps the participant list.
func (i LiveNoteMeetingInfo) rosterMembers() []liveNoteMember {
	order := make([]string, 0, 8)
	byName := make(map[string]*liveNoteMember, 8)
	add := func(name string) *liveNoteMember {
		if m, ok := byName[name]; ok {
			return m
		}
		m := &liveNoteMember{name: name, firstAt: len(order)}
		byName[name] = m
		order = append(order, name)
		return m
	}

	for _, s := range i.Segments {
		label := strings.TrimSpace(s.Speaker)
		if label == "" {
			label = strings.TrimSpace(s.Channel)
		}
		if !liveNoteLabelHasName(label) {
			continue
		}
		add(label).lines++
	}
	for _, s := range i.Screens {
		name := strings.TrimSpace(liveNoteScreenName(s))
		if !liveNoteLabelHasName(name) {
			continue
		}
		add(name).screen = true
	}

	members := make([]liveNoteMember, 0, len(order))
	for _, name := range order {
		members = append(members, *byName[name])
	}
	sort.SliceStable(members, func(a, b int) bool {
		if members[a].lines != members[b].lines {
			return members[a].lines > members[b].lines
		}
		return members[a].firstAt < members[b].firstAt
	})
	if len(members) > liveNoteMaxParticipants {
		members = members[:liveNoteMaxParticipants]
	}
	return members
}

// label renders one member with their role in the meeting.
func (m liveNoteMember) label(selfLabel string) string {
	var notes []string
	if selfLabel != "" && m.name == selfLabel {
		notes = append(notes, "发起人")
	}
	if m.lines > 0 {
		notes = append(notes, fmt.Sprintf("发言 %d 段", m.lines))
	}
	if m.screen {
		notes = append(notes, "共享过屏幕")
	}
	if len(notes) == 0 {
		return m.name
	}
	return m.name + "（" + strings.Join(notes, "，") + "）"
}

// ── Key frames ──────────────────────────────────────────────────────────────

// liveNoteShotPromptMax bounds how many frames reach the prompt. A hundred
// timestamps is noise; a sample is a map.
const liveNoteShotPromptMax = 60

// liveNoteShotReasonText is the prompt wording for a capture trigger.
//
// Deliberately not localized through liveNoteT: unlike the file/screen lines
// these never reach the reader — they exist only inside the Chinese prompt, and
// adding eight i18n keys for one consumer would be vocabulary nobody translates.
func liveNoteShotReasonText(reason string) string {
	switch reason {
	case LiveNoteShotReasonManual:
		return "手动截图"
	case LiveNoteShotReasonFile:
		return "打开文件"
	case LiveNoteShotReasonSharer:
		return "成员共享"
	case LiveNoteShotReasonSwitch:
		return "切换共享"
	case LiveNoteShotReasonSpeech:
		return "有人发言"
	default:
		return "画面变化"
	}
}

// LiveNoteShotsPromptSection renders the key-frame timeline for the minutes.
//
// This is the one place the pictures enter the summary, and only as timestamps:
// the frames themselves are never sent to the model. What they buy is the
// referent for 「这个」「这里」 — a transcript line saying "看这个报错" is
// unreadable until the reader knows a screen was being shared at that moment.
//
// The wording is defensive for the same reason the file timeline's is: a model
// handed a list of moments will happily write "the team reviewed the deploy log"
// from the mere fact that a frame exists.
func LiveNoteShotsPromptSection(shots []LiveNoteShot) string {
	if len(shots) == 0 {
		return ""
	}
	sampled := sampleLiveNoteShots(shots, liveNoteShotPromptMax)
	rows := make([]string, 0, len(sampled))
	for _, s := range sampled {
		if s.At <= 0 {
			continue
		}
		label := liveNoteShotReasonText(s.Reason)
		// Only a file trigger carries a name worth reading; other refs are
		// internal ids.
		if s.Reason == LiveNoteShotReasonFile {
			if name := liveNoteBaseName(s.Ref); name != "" {
				label += " · " + name
			}
		}
		rows = append(rows, fmt.Sprintf("- %s %s", liveNoteClock(s.At), label))
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 共享画面（关键帧）\n")
	b.WriteString("以下是会议期间自动截取画面的时间点（按时间排序）。它们说明「当时大家在看什么」，")
	b.WriteString("但不是发言内容：不要仅凭某个画面出现就推断结论、决定或待办。\n")
	if len(shots) > len(sampled) {
		fmt.Fprintf(&b, "（共 %d 个画面，以下为均匀抽样）\n", len(shots))
	}
	b.WriteString("\n")
	for _, row := range rows {
		b.WriteString(row)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// sampleLiveNoteShots keeps at most n frames, evenly spaced, always including
// the first and the last.
func sampleLiveNoteShots(shots []LiveNoteShot, n int) []LiveNoteShot {
	if n <= 0 || len(shots) <= n {
		return shots
	}
	if n == 1 {
		return shots[:1]
	}
	out := make([]LiveNoteShot, 0, n)
	for i := range n {
		idx := i * (len(shots) - 1) / (n - 1)
		out = append(out, shots[idx])
	}
	return out
}

// ── Chat ────────────────────────────────────────────────────────────────────

// liveNoteChatPromptMaxMessages / Runes bound the chat block.
const (
	liveNoteChatPromptMaxMessages = 120
	liveNoteChatPromptMaxRunes    = 6000
)

// LiveNoteChatPromptSection renders the meeting chat for the minutes.
//
// Chat is where the decisions that nobody says out loud end up: a link, a stack
// trace, "这条先合了吧". Leaving it out meant the minutes summarized a meeting
// where half the conclusions were typed.
func LiveNoteChatPromptSection(msgs []LiveNoteChatMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	sep := liveNoteT("MeetingTranscriptSeparator")
	rows := make([]string, 0, len(msgs))
	budget := liveNoteChatPromptMaxRunes
	for i, m := range msgs {
		if i >= liveNoteChatPromptMaxMessages || budget <= 0 {
			break
		}
		if m.At <= 0 {
			continue
		}
		text := liveNoteOneLine(m.Text)
		if text == "" {
			continue
		}
		speaker := liveNoteOneLine(m.Speaker)
		if speaker == "" {
			speaker = liveNoteT("MeetingChatAnonymous")
		}
		row := liveNoteClock(m.At) + " " + speaker + sep + text
		runes := utf8.RuneCountInString(row)
		if runes > budget {
			break
		}
		budget -= runes
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 会议聊天记录\n")
	b.WriteString("以下是参会人在会议聊天里发的文字（与语音讨论并列，同属会议内容；链接、报错和结论常在这里）：\n\n")
	for _, row := range rows {
		b.WriteString(row)
		b.WriteString("\n")
	}
	if len(rows) < len(msgs) {
		b.WriteString("（聊天记录较多，以上为前 " + fmt.Sprint(len(rows)) + " 条）\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ── Previous meeting ────────────────────────────────────────────────────────

// liveNoteTodoSectionMaxRunes bounds the carried-over action items.
const liveNoteTodoSectionMaxRunes = 1200

// PreviousLiveNoteMinutes returns the most recent earlier meeting of the same
// project that already has minutes.
//
// Scoped by project_id, not by path: a meeting belongs to a project, and the
// project's directory can be renamed without losing its history.
func PreviousLiveNoteMinutes(projectID, beforeMS int64, excludeToken string) (LiveNote, bool, error) {
	if projectID <= 0 || beforeMS <= 0 {
		return LiveNote{}, false, nil
	}
	var note LiveNote
	err := store.ReadDB().QueryRow(
		`SELECT token, title, minutes, started_at, ended_at
		   FROM live_notes
		  WHERE project_id = ? AND token <> ? AND minutes <> '' AND started_at > 0 AND started_at < ?
		  ORDER BY started_at DESC
		  LIMIT 1`,
		projectID, excludeToken, beforeMS,
	).Scan(&note.Token, &note.Title, &note.Minutes, &note.StartedAt, &note.EndedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LiveNote{}, false, nil
		}
		return LiveNote{}, false, fmt.Errorf("live notes: previous meeting: %w", err)
	}
	return note, true, nil
}

// LiveNotePreviousTodosPromptSection carries the previous meeting's action items
// into this one.
//
// Only the 「待办事项」 section travels, not the whole minutes: the point is
// "what was left over", and a full previous summary would be read as context for
// this meeting's discussion.
func LiveNotePreviousTodosPromptSection(prev LiveNote) string {
	todos := liveNoteTodoSection(prev.Minutes)
	if todos == "" {
		return ""
	}
	when := ""
	if prev.StartedAt > 0 {
		when = time.UnixMilli(prev.StartedAt).Format("2006-01-02") + " "
	}
	title := strings.TrimSpace(prev.Title)
	if title != "" {
		when += "《" + title + "》"
	}

	var b strings.Builder
	b.WriteString("## 上一次会议的待办事项\n")
	b.WriteString("上一次会议（" + strings.TrimSpace(when) + "）留下的待办：\n\n")
	b.WriteString(todos)
	b.WriteString("\n\n这些是上一次会议的内容，不是本次会议的发言。只有本次讨论明确提到某一项时，")
	b.WriteString("才把它写进「遗留事项」章节并说明进展；本次会议与它们无关时，省略该章节。")
	return b.String()
}

// liveNoteTodoSection extracts the action-item section from generated minutes.
//
// Heading-based because the minutes prompt fixes the section names; a model that
// renamed the heading still gets a chance through the keyword list. When nothing
// matches, "" — the whole minutes are not a substitute.
func liveNoteTodoSection(minutes string) string {
	lines := strings.Split(minutes, "\n")
	out := make([]string, 0, 8)
	in := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if in {
				break
			}
			in = liveNoteIsTodoHeading(trimmed)
			continue
		}
		if in && trimmed != "" {
			out = append(out, trimmed)
		}
	}
	text := strings.TrimSpace(strings.Join(out, "\n"))
	if text == "" {
		return ""
	}
	return truncateRunes(text, liveNoteTodoSectionMaxRunes)
}

// liveNoteIsTodoHeading reports whether a Markdown heading names the action
// items.
func liveNoteIsTodoHeading(heading string) bool {
	l := strings.ToLower(heading)
	for _, keyword := range []string{"待办", "行动项", "遗留", "action item", "todo", "to-do", "follow-up"} {
		if strings.Contains(l, keyword) {
			return true
		}
	}
	return false
}

// ── Minutes context assembly ────────────────────────────────────────────────

// LiveNoteMinutesContext is everything the minutes prompt gets besides the
// transcript itself.
//
// One struct, one renderer: the order of these blocks is a decision (framing,
// then content, then what was on screen, then history), and scattering it across
// the caller is how the previous meeting ends up quoted before the meeting.
type LiveNoteMinutesContext struct {
	Info    LiveNoteMeetingInfo
	Chat    []LiveNoteChatMessage
	Files   []LiveNoteFile
	Screens []LiveNoteScreenShare
	Shots   []LiveNoteShot
	Prev    LiveNote
	HasPrev bool
}

// PromptSection renders the whole block, or "" when the meeting has nothing
// beyond its transcript.
func (c LiveNoteMinutesContext) PromptSection() string {
	sections := make([]string, 0, 5)
	if s := c.Info.PromptSection(); s != "" {
		sections = append(sections, s)
	}
	if s := LiveNoteChatPromptSection(c.Chat); s != "" {
		sections = append(sections, s)
	}
	timeline := LiveNoteFilesPromptSection(c.Files, c.Info.startTime(), c.Info.ProjectPath)
	if s := LiveNoteScreenPromptSection(c.Screens, c.Info.startTime()); s != "" {
		if timeline != "" {
			timeline += "\n\n"
		}
		timeline += s
	}
	if s := LiveNoteShotsPromptSection(c.Shots); s != "" {
		if timeline != "" {
			timeline += "\n\n"
		}
		timeline += s
	}
	if timeline != "" {
		sections = append(sections, timeline)
	}
	if c.HasPrev {
		if s := LiveNotePreviousTodosPromptSection(c.Prev); s != "" {
			sections = append(sections, s)
		}
	}
	return strings.Join(sections, "\n\n")
}

// startTime is the meeting's start as a Time, or the zero value.
func (i LiveNoteMeetingInfo) startTime() time.Time {
	if i.StartedAt <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(i.StartedAt)
}
