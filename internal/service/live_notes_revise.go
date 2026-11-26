package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

// liveNotesReviseChunkRunes is the per-chunk input budget for proofreading.
//
// It is deliberately small: the proofreader has to locate every replacement in
// the text it is given, and a chunk large enough to lose track of a line is a
// chunk whose corrections land in the wrong place.
const liveNotesReviseChunkRunes = 6000

// liveNotesReviseMaxTokens bounds one proofread answer.
//
// The answer is a list of `原词 => 改词` lines, not a rewritten transcript, so a
// few hundred tokens is the honest ceiling. Keeping the cap far below the chunk
// size is intentional: a model that ignores the format and starts echoing the
// transcript gets cut off, and a cut-off answer is skipped rather than applied
// half-way — the cap is what keeps a runaway answer from being usable.
const liveNotesReviseMaxTokens = 2000

// liveNoteReviseNoChanges is what a chunk answers when it found nothing to fix.
//
// Without a sentinel, "no corrections" and "the model ignored the format" look
// identical — both are answers with no replacement pair in them. The sentinel is
// what keeps the second one distinguishable, so a chunk that answered in prose
// is not recorded as "checked, nothing to fix".
const liveNoteReviseNoChanges = "NO_CHANGES"

// liveNoteRevisePairMaxRunes bounds one side of a replacement pair. A pair is a
// word or a short phrase; anything longer is a paraphrase, which this stage is
// not allowed to produce.
const liveNoteRevisePairMaxRunes = 60

// liveNotesReviseLogRunes bounds how much of a rejected answer reaches the log.
//
// Long enough to show the shape of the answer (a few lines) and short enough
// that a model that echoed the whole chunk cannot flood the log with meeting
// content.
const liveNotesReviseLogRunes = 600

// liveNoteSplitMinRunes is how long the phrase after `==>` must be.
//
// Longer than a replacement pair's minimum because the phrase does two jobs
// here: it locates the line (the `[mm:ss]` anchor only narrows it to one cue)
// and it is where the new line is cut. A two-rune phrase is a cut in the wrong
// place far more often than it is a speaker change.
const liveNoteSplitMinRunes = 4

// liveNoteSplitSpeakerMaxRunes bounds a synthesized speaker label. A label is a
// name or a form of address; anything longer is the model explaining itself.
const liveNoteSplitSpeakerMaxRunes = 20

// ErrLiveNoteReviseFormat is returned when a chunk's answer is not a list of
// replacement pairs.
//
// One such chunk is no longer fatal. The pair format is what guarantees every
// timestamp, speaker and line survives, so an answer in some other shape simply
// contributes no edits — the chunk's raw text is already what the output keeps,
// and the other chunks' corrections are unaffected. Failing the whole meeting
// for it threw away every good chunk too, which is a worse trade than a chunk
// that stays unproofread.
//
// It is still returned when EVERY chunk answered unusably: that is a model or
// prompt problem, and a "proofread" that proofread nothing should say so rather
// than hand back the raw transcript as if it had worked.
var ErrLiveNoteReviseFormat = errors.New("live notes: proofreader did not answer with replacement pairs")

// liveNotesRevisePrompt turns the proofreader into a diff generator.
//
// It used to ask for the rewritten transcript. That made the model's obedience
// to the line format load-bearing — the file-event merge afterwards keys on the
// `[mm:ss]` prefix, so a model that reflowed a line silently broke the timeline —
// and it made the same misheard word come out spelled differently in two chunks,
// because each chunk was rewritten in isolation.
//
// Asking for pairs fixes both: the transcript is never rewritten, so every
// timestamp and label survives by construction, and the pairs from all chunks
// are applied once over the whole meeting, so one word gets one spelling.
//
// The instructions are otherwise deliberately narrow. A proofreader allowed to
// "improve" a sentence rewrites the meeting, and the minutes then describe what
// the model thinks should have been said.
//
// People are the one place where the transcript carries its own vocabulary:
// each line is prefixed with the speaker label, and that label is the 称谓 the
// person entered — the publisher's included (web/src/components/live/
// MeetingCreate.vue). So a name mangled in the body can be checked against the
// labels without any extra input. Legacy transcripts still carry a pronoun or
// an ordinal there (「我」/「Me」/「观众 3」), which is why rule 4 forbids
// inventing a name from a label that has none. Forms of address get their own
// rule because "王总" and "王小明" are the same person in two registers: the
// surname is correctable, the honorific is not the model's to rewrite.
//
// The label is a channel, not a person. A channel is one microphone, and a
// microphone can have a room in front of it: someone repeats what a colleague
// beside them said, or two people share a laptop. The prompt therefore carries
// a second output shape — `[mm:ss] 原词 ==> 新说话人` — because a replacement
// pair cannot express this: replacing text is not the same edit as inserting a
// line. It stays a deliberate exception, gated on three conditions that all have
// to hold, because a proofreader free to re-diarize the meeting is a
// proofreader free to invent participants.
const liveNotesRevisePrompt = `你是一名会议转写校对员。输入是语音识别的会议转写，每行格式为「[mm:ss] 说话人：内容」。

你的任务只有两件：找出听写错误，以「替换对」输出；一行里明显不止一个人在说话时，以「换人」输出把它分开。不要重写转写。

输出格式（每行一条，除此之外不要输出任何内容）：
原词 => 改词
[mm:ss] 原词 ==> 新说话人

1. 只改明显的同音/近音错字、专有名词、产品名、技术术语、人名、称呼与缩写。
2. 「术语表」和「项目背景」里出现过的写法是唯一正确写法：转写里与之接近的一律改成它（例如把同音词改成项目里的真实名字）。
3. 行首的说话人标签就是本人的称谓，是人名的权威写法：正文里明显指同一个人的近音写法，改成标签里的名字。标签本身不要动。
4. 行首标签若是代词或占位（「我」「Me」「观众 3」这类），它不含姓名信息：不要据此推断或改写正文，也不要为任何说话人补上转写里没有的名字。
5. 称呼（「王总」「李工」「张老师」这类）保留称谓本身，只按第 3 条校正其中的姓或名；不要把称呼整体换成姓名，也不要把姓名改成称呼。
6. 原词必须是转写里原样连续出现的片段，至少两个字（一个完整英文单词算一条）；单个汉字不足以定位，要连同前后各一个字一起给出。
7. 不要润色、不要改写句式、不要合并或拆分句子、不要补全没听清的内容；不要删除任何信息（口头语也保留）。
8. 听不清、无法确定、或者只是「可能」的，不要输出这一条；拿不准正文里的人名是不是行首那个人时，同样不要输出。
9. 保持原语言：英文术语保持英文，不要翻译成中文；中文也保持中文。
10. 如果这一段没有任何需要修改的地方，只输出 NO_CHANGES。

关于「一行里可能不止一个人」：
行首标签只说明这一路声音是从谁的麦克风来的，不保证这一行只有一个人在说话——同一个麦克风前可能坐着好几个人，也可能有人替旁边的人转述。但不要凭猜测拆人，只有下面三条同时成立，才用「换人」：
- 有明显换人迹象：一问答、被点名的人应声、自我介绍、互相称呼，这些只可能出自另一个人。
- 能确定新说话人是谁，而且这个名字在转写里已经出现过（别的行首标签，或正文里被叫到的名字）。转写里没有的名字不要编。
- 新说话人开口的第一句在转写里原样连续出现，至少四个字，长到足以定位换人的位置。
「换人」的写法：
- 行首 [mm:ss] 照抄这一行原来的时间，一个字都不要改。
- 原词写新说话人说的第一句话的开头，不要带行首标签，也不要把后面的话再重复一遍。
- 新说话人只写姓名或称谓，不要带冒号（「：」或「:」）。
- 时间不变：拆开后两行共用同一个 [mm:ss]，后半句的内容保持原样，原来的标签也不要动。
- 只是换了语气、停顿、或者你判断还是同一个人，不要拆。

不要输出转写原文，不要解释，不要加代码块。`

const liveNotesReviseQuestion = "请输出这一段转写的替换对；如果一行里明显有换人，一并输出换人行。"

// liveNotePair is one substitution the proofreader is confident about.
type liveNotePair struct {
	From string
	To   string
}

// liveNoteSplit is one speaker change the proofreader is confident about.
//
// It is a second edit shape rather than a pair because the transcript's own
// format cannot say "two people share this label", and a pair only replaces
// text. Clock is the `[mm:ss]` of the line the change belongs to: it is what
// makes the edit locatable across chunks, and it is deliberately reused for
// both halves — the two people spoke inside one cue, so inventing a new
// timestamp for the second one would be a lie about the timeline.
type liveNoteSplit struct {
	// Clock is the line's `[mm:ss]` prefix, brackets stripped.
	Clock string
	// From is the first words of the new speaker's turn: the cut goes here.
	From string
	// Speaker opens the new line. It must already exist in the transcript.
	Speaker string
}

// LiveNotesReviseInput is everything the proofreading stage reads.
//
// A struct rather than a parameter list because the stage grew from "segments
// and a project path" to five inputs, three of which exist only to build the
// glossary — and a six-argument call site is where the wrong one gets passed.
type LiveNotesReviseInput struct {
	// ProjectPath is the project root; its documents supply the vocabulary.
	ProjectPath string
	// Title is the meeting's name. A real title is a free domain term (「发布
	// 流程复盘」), which is why it is no longer an unused parameter.
	Title string
	// Files are the documents opened during the meeting: their base names are
	// the closest vocabulary to what was actually being discussed.
	Files []LiveNoteFile
	// Segments are the raw ASR rows.
	Segments []LiveNoteSegment
	// Self identifies the user behind the transcript's self label.
	Self LiveNotesSpeaker
}

// ReviseLiveNotesTranscript proofreads the raw transcript against the meeting's
// own vocabulary and returns the corrected timeline.
//
// Why this stage exists: ASR has no idea that "claw bench" is ClawBench or that
// "ACP" is not "a c p". The project's documents, the files people opened and the
// meeting's own title are full of the correct spellings, and feeding them to the
// proofreader fixes exactly the errors a human would fix — which is what makes
// the minutes readable.
//
// Chunked because a one-hour meeting does not fit in one request. Chunks are cut
// on line boundaries so a line is never split, and the corrections are applied
// afterwards in one pass so a chunk boundary cannot produce two spellings of the
// same word.
func ReviseLiveNotesTranscript(
	ctx context.Context, in LiveNotesReviseInput, progress ...func(done, total int),
) (string, error) {
	// Agent mode: same pairs, same applier, but the model may READ the project
	// to check how a term is actually spelled. Falls back to the summary path
	// when the agent is unavailable — a meeting the user is waiting on must not
	// fail because a background capability is off.
	if model.ConfigInstance.Meeting.AgentMinutes {
		if workDir := strings.TrimSpace(in.ProjectPath); workDir != "" && BuiltinAgentAvailable() {
			return reviseLiveNotesTranscriptWithAgent(ctx, in, workDir, progress...)
		}
	}

	transcript := BuildLiveNotesTranscript(in.Segments)
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}

	background := strings.Join(projectContext(in.ProjectPath), "\n\n")
	glossary := LiveNoteGlossaryPromptSection(in.Title, in.Files, in.Segments, in.ProjectPath)
	// Resolved once: every chunk gets the same initiator, and a chunk that
	// happens to contain no 「我」 line still needs to recognize the name.
	systemPrompt := liveNotesRevisePrompt
	if note := in.Self.reviseNote(); note != "" {
		systemPrompt += "\n\n" + note
	}

	chunks := splitRunes(transcript, liveNotesReviseChunkRunes)
	pairs, splits, err := proofreadLiveNoteChunks("revise", chunks, func(i, total int, chunk string) (string, error) {
		return summarize.AskAboutContextLossless(ctx, summarizer, systemPrompt,
			liveNoteReviseBlock(background, glossary, chunk), liveNoteChunkQuestion(i, total), liveNotesReviseMaxTokens)
	}, progress)
	if err != nil {
		return "", err
	}
	return applyLiveNoteEdits(transcript, pairs, splits), nil
}

// liveNoteChunkQuestion numbers a chunk when the transcript has more than one,
// so the model knows it is reading part of a longer meeting.
func liveNoteChunkQuestion(i, total int) string {
	if total <= 1 {
		return liveNotesReviseQuestion
	}
	return fmt.Sprintf("这是第 %d/%d 段。%s", i+1, total, liveNotesReviseQuestion)
}

// proofreadLiveNoteChunks asks for every chunk's edits and merges them.
//
// label names the pass in the error ("revise" / "agent revise") so the message
// still says which stage failed and at which chunk. A chunk that cannot be used
// is skipped rather than aborting: its raw text is already in the output
// verbatim, so the only thing lost is that chunk's corrections. Only when EVERY
// chunk is unusable does the pass fail, because then nothing was proofread and
// reporting success would be a lie.
func proofreadLiveNoteChunks(
	label string, chunks []string,
	ask func(i, total int, chunk string) (string, error),
	progress []func(done, total int),
) ([]liveNotePair, []liveNoteSplit, error) {
	pairs := make([]liveNotePair, 0, len(chunks)*8)
	splits := make([]liveNoteSplit, 0, 2)
	unusable := 0
	var firstUnusable error
	var firstUnusableChunk int
	for i, chunk := range chunks {
		answer, err := ask(i, len(chunks), chunk)
		// Truncation is the one call error that is not fatal: the answer is
		// still a prefix of the pair list, so it is skipped like an unusable
		// one. Everything else (transport, auth, model) aborts the pass.
		if err != nil && !errors.Is(err, summarize.ErrOutputTruncated) {
			return nil, nil, fmt.Errorf("live notes: %s chunk %d: %w", label, i+1, err)
		}
		chunkPairs, chunkSplits, skipErr := liveNoteChunkEdits(i+1, len(chunks), answer, err)
		if skipErr != nil {
			unusable++
			if firstUnusable == nil {
				firstUnusable, firstUnusableChunk = skipErr, i+1
			}
		} else {
			pairs = append(pairs, chunkPairs...)
			splits = append(splits, chunkSplits...)
		}
		if len(progress) > 0 && progress[0] != nil {
			progress[0](i+1, len(chunks))
		}
	}
	if unusable == len(chunks) {
		return nil, nil, fmt.Errorf("live notes: %s chunk %d: %w", label, firstUnusableChunk, firstUnusable)
	}
	return pairs, splits, nil
}

// liveNoteChunkEdits turns one chunk's answer into edits, or reports that this
// chunk has to be left unproofread.
//
// Two failures stop here instead of aborting the meeting, because both are
// confined to this chunk's corrections: an answer in the wrong shape, and an
// answer cut off at the output cap — a truncated list's last line can be half a
// pair (`原词 => 半截改词`), and writing that fragment into the transcript is
// worse than losing the chunk's corrections. The transcript of a skipped chunk
// is already in the output verbatim, so nothing is lost but the corrections.
//
// A transport or model error is NOT handled here: the caller aborts on it.
func liveNoteChunkEdits(chunk, chunks int, answer string, callErr error) ([]liveNotePair, []liveNoteSplit, error) {
	if callErr != nil {
		slog.Warn("live notes: proofread answer truncated, chunk left unproofread",
			"chunk", chunk, "chunks", chunks, "err", callErr)
		return nil, nil, callErr
	}
	pairs, splits, err := parseLiveNoteAnswer(answer)
	if err != nil {
		// The answer itself goes to the log: the error only carries one line,
		// and diagnosing "the model ignored the format" needs to see what it
		// wrote instead.
		slog.Warn("live notes: unusable proofread answer, chunk left unproofread",
			"chunk", chunk, "chunks", chunks, "err", err,
			"answer", truncateRunes(answer, liveNotesReviseLogRunes))
		return nil, nil, err
	}
	return pairs, splits, nil
}

// liveNoteReviseBlock frames one chunk for the proofreader.
//
// The transcript always gets its own heading, even with no context at all:
// without it the model has no delimiter between "context" and "the thing to
// correct", and a project without an AGENTS.md is the normal case for a fresh
// directory.
func liveNoteReviseBlock(background, glossary, chunk string) string {
	var b strings.Builder
	if glossary != "" {
		b.WriteString(glossary)
		b.WriteString("\n\n")
	}
	if background != "" {
		b.WriteString("## 项目背景（专有名词以此为准）\n")
		b.WriteString(background)
		b.WriteString("\n\n")
	}
	b.WriteString("## 待校对转写\n")
	b.WriteString(chunk)
	return b.String()
}

// liveNoteReject is one answer line that produced no edit, with the reason.
//
// The reason is kept because the failure it explains is read by a person, not a
// program: the error lands in the UI as 「校对稿生成失败：…」 and the only
// actionable thing in it is what the model actually wrote. The previous version
// quoted only lines with no `=>`, so an answer whose every line carried an arrow
// reported `(first line: "")` — the answer closest to the format was the one
// that said nothing about itself.
type liveNoteReject struct {
	Line   string
	Reason string
}

// parseLiveNoteAnswer reads one chunk's answer as replacement pairs and speaker
// splits.
//
// Formatting noise is tolerated — list markers, backticks, a code fence, stray
// commentary lines — because those are the same answer wearing a different
// jacket. A shape that carries no edit at all is not: see ErrLiveNoteReviseFormat.
//
// A line containing `==>` is always read as a split attempt and never falls
// through to the pair parser: a split that cannot be applied is dropped, not
// turned into "the model ignored the format". The distinction matters because
// the format error aborts the whole meeting, and a speaker change the model
// phrased badly is not evidence that its corrections are unusable.
func parseLiveNoteAnswer(raw string) ([]liveNotePair, []liveNoteSplit, error) {
	text := stripLiveNoteCodeFence(strings.TrimSpace(raw))
	if text == "" {
		return nil, nil, fmt.Errorf("%w (empty answer)", ErrLiveNoteReviseFormat)
	}
	if strings.EqualFold(strings.TrimRight(text, "。.！!"), liveNoteReviseNoChanges) {
		return nil, nil, nil
	}

	pairs := make([]liveNotePair, 0, 8)
	splits := make([]liveNoteSplit, 0, 2)
	var rejected []liveNoteReject
	for _, line := range strings.Split(text, "\n") {
		line = trimLiveNoteListMarker(strings.TrimSpace(line))
		if line == "" || strings.EqualFold(line, liveNoteReviseNoChanges) {
			continue
		}
		if strings.Contains(line, "==>") {
			split, reason := parseLiveNoteSplitLine(line)
			if reason != "" {
				rejected = append(rejected, liveNoteReject{Line: line, Reason: reason})
				continue
			}
			splits = append(splits, split)
			continue
		}
		pair, isPair, reason := parseLiveNotePairLine(line)
		if !isPair {
			rejected = append(rejected, liveNoteReject{Line: line, Reason: "no `=>` in the line"})
			continue
		}
		if reason != "" {
			rejected = append(rejected, liveNoteReject{Line: line, Reason: reason})
			continue
		}
		pairs = append(pairs, pair)
	}
	if len(pairs) == 0 && len(splits) == 0 {
		return nil, nil, liveNoteFormatError(rejected)
	}
	return pairs, splits, nil
}

// liveNoteFormatError builds the failure for a chunk whose answer yielded no
// edit at all.
//
// It always names one real line and why it was rejected: "the model ignored the
// format" is a conclusion the reader has to be able to check, and a chunk number
// alone is not enough to tell a wrong arrow from a wrong model.
func liveNoteFormatError(rejected []liveNoteReject) error {
	if len(rejected) == 0 {
		return fmt.Errorf("%w (no usable line in the answer)", ErrLiveNoteReviseFormat)
	}
	detail := fmt.Sprintf("%d unusable line(s); first: %q — %s",
		len(rejected), truncateRunes(rejected[0].Line, 60), rejected[0].Reason)
	if hint := liveNoteRejectHint(rejected); hint != "" {
		detail += "; " + hint
	}
	return fmt.Errorf("%w (%s)", ErrLiveNoteReviseFormat, detail)
}

// liveNoteRejectHint names a whole-answer pattern that no single line shows.
//
// The one worth naming is an answer that is nothing but clock-less `==>` lines:
// that is the model using the speaker-split arrow for plain replacements, which
// reads exactly like a model that ignored the format but is fixed by a different
// decision (sharpen the prompt, or retry) than a model answering in prose.
func liveNoteRejectHint(rejected []liveNoteReject) string {
	for _, r := range rejected {
		if !strings.Contains(r.Line, "==>") {
			return ""
		}
	}
	return "every line of the answer is a clock-less `==>` line, so the model is probably using the speaker-split arrow for plain replacements"
}

// parseLiveNotePairLine reads one `原词 => 新词` replacement line.
//
// It reports three things, because the caller must treat the two failure
// modes differently:
//
//   - isPair: the line actually carries a `=>` separator. A line without one is
//     not a malformed pair, it is a line the model did not answer in the
//     requested format — and that IS a format error.
//   - reason: why the pair cannot be applied, empty when it can. A pair that
//     cannot be applied is dropped rather than fatal on its own: the rest of
//     the chunk's corrections are still good, and the raw text is what survives
//     for this one.
//
// A pair is applicable when both sides are non-empty, differ from each other,
// the source side is at least two runes long, and neither side exceeds
// liveNoteRevisePairMaxRunes.
func parseLiveNotePairLine(line string) (pair liveNotePair, isPair bool, reason string) {
	left, right, found := strings.Cut(line, "=>")
	if !found {
		return liveNotePair{}, false, ""
	}
	from := liveNotePairSide(left)
	to := liveNotePairSide(right)
	switch {
	case from == "":
		return liveNotePair{}, true, "empty original"
	case to == "":
		return liveNotePair{}, true, "empty replacement"
	case from == to:
		return liveNotePair{}, true, "original and replacement are the same"
	}
	if n := utf8.RuneCountInString(from); n < 2 {
		return liveNotePair{}, true, fmt.Sprintf("original is %d rune, needs 2 to be locatable", n)
	} else if n > liveNoteRevisePairMaxRunes {
		return liveNotePair{}, true, fmt.Sprintf("original is %d runes, the limit is %d", n, liveNoteRevisePairMaxRunes)
	}
	if n := utf8.RuneCountInString(to); n > liveNoteRevisePairMaxRunes {
		return liveNotePair{}, true, fmt.Sprintf("replacement is %d runes, the limit is %d", n, liveNoteRevisePairMaxRunes)
	}
	return liveNotePair{From: from, To: to}, true, ""
}

// parseLiveNoteSplitLine reads one `[mm:ss] 原词 ==> 新说话人` line.
//
// The clock is required rather than optional: it is the only thing that keeps a
// split from being applied to a line that merely happens to contain the same
// words, and every line in the transcript carries one. The reason string is
// empty when the split is usable.
func parseLiveNoteSplitLine(line string) (liveNoteSplit, string) {
	clock, body, ok := liveNoteClockBody(line)
	if !ok {
		return liveNoteSplit{}, "`==>` line with no leading [mm:ss], so the cut has no place on the timeline"
	}
	left, right, ok := strings.Cut(body, "==>")
	if !ok {
		return liveNoteSplit{}, "`==>` line with no arrow"
	}
	from := liveNotePairSide(left)
	if n := utf8.RuneCountInString(from); n < liveNoteSplitMinRunes {
		return liveNoteSplit{}, fmt.Sprintf("the words to cut at are %d rune(s), the minimum is %d", n, liveNoteSplitMinRunes)
	} else if n > liveNoteRevisePairMaxRunes {
		return liveNoteSplit{}, fmt.Sprintf("the words to cut at are %d runes, the limit is %d", n, liveNoteRevisePairMaxRunes)
	}
	speaker := liveNoteSplitSpeaker(right)
	if speaker == "" {
		return liveNoteSplit{}, "no usable speaker label after `==>`"
	}
	return liveNoteSplit{Clock: clock, From: from, Speaker: speaker}, ""
}

// liveNoteSplitSpeaker normalizes a synthesized speaker label, or "" when it
// cannot be used as one.
//
// A colon is rejected rather than escaped: the label ends at the first
// separator, so a label containing one would silently move the boundary and put
// half the label into the spoken text. Brackets are rejected for the same
// reason on the `[mm:ss]` side.
func liveNoteSplitSpeaker(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'“”‘’「」")
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "：:\r\n[]") {
		return ""
	}
	if utf8.RuneCountInString(s) > liveNoteSplitSpeakerMaxRunes {
		return ""
	}
	return s
}

// liveNoteClockBody splits a transcript line into its `[mm:ss]` clock (brackets
// stripped) and everything after it.
//
// Unlike liveNoteTranscriptLine this accepts `[--:--]` too: a split inside a
// line whose timing is unknown is still a real speaker change, and the second
// half has to keep the same unknown timing rather than acquire a made-up one.
func liveNoteClockBody(line string) (clock, body string, ok bool) {
	if !strings.HasPrefix(line, "[") {
		return "", "", false
	}
	end := strings.Index(line, "]")
	if end <= 1 {
		return "", "", false
	}
	clock = strings.TrimSpace(line[1:end])
	if clock == "" {
		return "", "", false
	}
	return clock, strings.TrimLeft(line[end+1:], " "), true
}

// stripLiveNoteCodeFence removes a ``` wrapper if the model added one.
func stripLiveNoteCodeFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = text[idx+1:]
	}
	if idx := strings.LastIndex(text, "```"); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

// trimLiveNoteListMarker drops a leading "- ", "* " or "1. ".
func trimLiveNoteListMarker(line string) string {
	if len(line) > 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return strings.TrimSpace(line[2:])
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(line) && (line[i] == '.' || line[i] == ')') && line[i+1] == ' ' {
		return strings.TrimSpace(line[i+2:])
	}
	return line
}

// liveNotePairSide normalizes one side of a pair: the model may quote it.
func liveNotePairSide(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'“”‘’「」")
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\r\n") {
		return ""
	}
	return s
}

// applyLiveNoteEdits applies both kinds of edit, splits first.
//
// Order matters: a split's anchor is the raw text the model read, so it has to
// be located before the replacement pairs rewrite it. The pairs then run over
// the already-split transcript, which is what lets a correction land in the
// second half of a line that was just separated.
func applyLiveNoteEdits(transcript string, pairs []liveNotePair, splits []liveNoteSplit) string {
	if len(splits) > 0 {
		transcript = applyLiveNoteSplits(transcript, splits)
	}
	return applyLiveNotePairs(transcript, pairs)
}

// applyLiveNoteSplits breaks lines where the proofreader says a new speaker
// starts.
//
// The inserted line reuses the clock of the line it came from: the two people
// spoke inside one cue, so the second half has no honest offset of its own. The
// speaker label of the original line is never touched — the split adds a line,
// it does not rename anybody.
func applyLiveNoteSplits(transcript string, splits []liveNoteSplit) string {
	if len(splits) == 0 {
		return transcript
	}
	sep := liveNoteT("MeetingTranscriptSeparator")
	resolved := resolveLiveNoteSplits(splits)
	lines := strings.Split(transcript, "\n")
	out := make([]string, 0, len(lines)+len(resolved))
	for _, line := range lines {
		out = append(out, splitLiveNoteLineAt(line, resolved, sep)...)
	}
	return strings.Join(out, "\n")
}

// splitLiveNoteLineAt cuts one line at every split that anchors to it.
//
// A cut with no spoken text before it is skipped: it would emit an empty line
// ahead of the new speaker, and a line whose whole body belongs to the second
// person is already what the label says.
func splitLiveNoteLineAt(line string, splits []liveNoteSplit, sep string) []string {
	clock, body, ok := liveNoteClockBody(line)
	if !ok || body == "" {
		return []string{line}
	}
	head := line[:len(line)-len(body)]
	// Everything up to and including the separator is the label, so a cut at
	// or before this offset leaves the first speaker with nothing to say.
	speechStart := 0
	label := ""
	if i := strings.Index(body, sep); i >= 0 {
		label = strings.TrimSpace(body[:i])
		speechStart = i + len(sep)
	}

	type cut struct {
		at      int
		speaker string
	}
	cuts := make([]cut, 0, len(splits))
	for _, s := range splits {
		if s.Clock != clock || s.Speaker == label {
			continue
		}
		idx := strings.Index(body, s.From)
		if idx <= speechStart || strings.TrimSpace(body[speechStart:idx]) == "" {
			continue
		}
		cuts = append(cuts, cut{at: idx, speaker: s.Speaker})
	}
	if len(cuts) == 0 {
		return []string{line}
	}
	sort.SliceStable(cuts, func(i, j int) bool { return cuts[i].at < cuts[j].at })

	out := make([]string, 0, len(cuts)+1)
	prev := 0
	prefix := head
	for _, c := range cuts {
		// A cut inside an earlier one would emit the same words twice.
		if c.at <= prev {
			continue
		}
		out = append(out, prefix+strings.TrimRight(body[prev:c.at], " "))
		prefix = "[" + clock + "] " + c.speaker + sep
		prev = c.at
	}
	return append(out, prefix+body[prev:])
}

// resolveLiveNoteSplits collapses duplicate splits.
//
// Two chunks cannot report the same split (a line is never cut across chunks),
// but one answer can repeat a line, and applying the same cut twice would open
// a second, empty turn for the same person. First-seen wins, like the pair
// resolver, so the result stays reproducible.
func resolveLiveNoteSplits(splits []liveNoteSplit) []liveNoteSplit {
	seen := make(map[string]bool, len(splits))
	out := make([]liveNoteSplit, 0, len(splits))
	for _, s := range splits {
		key := s.Clock + "\x00" + s.From
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// applyLiveNotePairs substitutes the resolved pairs into a transcript.
//
// It runs once over the whole meeting rather than per chunk, which is the entire
// point: two chunks can fix the same misheard word differently, and only a
// single resolved mapping can leave one spelling in the transcript.
//
// The speaker label is never touched — rule 3 makes it the authority for a
// person's name, so a pair must not be able to rewrite it.
func applyLiveNotePairs(transcript string, pairs []liveNotePair) string {
	if len(pairs) == 0 {
		return transcript
	}
	resolved := resolveLiveNotePairs(pairs)
	sep := liveNoteT("MeetingTranscriptSeparator")
	lines := strings.Split(transcript, "\n")
	for i, line := range lines {
		head, body := splitLiveNoteLine(line, sep)
		if body == "" {
			continue
		}
		for _, p := range resolved {
			if strings.Contains(body, p.From) {
				body = strings.ReplaceAll(body, p.From, p.To)
			}
		}
		lines[i] = head + body
	}
	return strings.Join(lines, "\n")
}

// splitLiveNoteLine separates a transcript line into the `[mm:ss] 说话人：`
// prefix and the spoken text.
//
// A line with no separator is all body: the prefix is what must survive, and a
// line that has none has nothing to protect.
func splitLiveNoteLine(line, sep string) (head, body string) {
	idx := strings.Index(line, sep)
	if idx < 0 {
		return "", line
	}
	end := idx + len(sep)
	return line[:end], line[end:]
}

// resolveLiveNotePairs collapses the pairs from every chunk into one mapping.
//
// Same original word, different corrections: the most frequent one wins and the
// first-seen one breaks the tie. Both rules are deterministic, so the same
// transcript and the same answers always produce the same text — which is what
// makes the result reproducible enough to store.
//
// The result is ordered longest-first so that "a c p" is replaced before "a c",
// and a longer phrase is never mangled by a shorter pair inside it.
func resolveLiveNotePairs(pairs []liveNotePair) []liveNotePair {
	type tally struct {
		order []string
		count map[string]int
	}
	tallies := make(map[string]*tally, len(pairs))
	froms := make([]string, 0, len(pairs))
	for _, p := range pairs {
		t, ok := tallies[p.From]
		if !ok {
			t = &tally{count: make(map[string]int, 2)}
			tallies[p.From] = t
			froms = append(froms, p.From)
		}
		if _, seen := t.count[p.To]; !seen {
			t.order = append(t.order, p.To)
		}
		t.count[p.To]++
	}

	out := make([]liveNotePair, 0, len(froms))
	for _, from := range froms {
		t := tallies[from]
		best := t.order[0]
		for _, to := range t.order[1:] {
			if t.count[to] > t.count[best] {
				best = to
			}
		}
		out = append(out, liveNotePair{From: from, To: best})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return utf8.RuneCountInString(out[i].From) > utf8.RuneCountInString(out[j].From)
	})
	return out
}
