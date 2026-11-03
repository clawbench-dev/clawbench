package service

import (
	"context"
	"errors"
	"fmt"
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
// transcript gets cut off (and therefore fails loudly) instead of returning a
// half-rewritten chunk nobody notices.
const liveNotesReviseMaxTokens = 2000

// liveNoteReviseNoChanges is what a chunk answers when it found nothing to fix.
//
// Without a sentinel, "no corrections" and "the model ignored the format" look
// identical — both are answers with no replacement pair in them. The sentinel is
// what makes the second one an error instead of a silent pass-through.
const liveNoteReviseNoChanges = "NO_CHANGES"

// liveNoteRevisePairMaxRunes bounds one side of a replacement pair. A pair is a
// word or a short phrase; anything longer is a paraphrase, which this stage is
// not allowed to produce.
const liveNoteRevisePairMaxRunes = 60

// ErrLiveNoteReviseFormat is returned when a chunk's answer is not a list of
// replacement pairs.
//
// Deliberately fatal rather than falling back to "keep the raw text": the pair
// format is what guarantees every timestamp, speaker and line survives, and a
// proofreader that answered in some other shape has not demonstrated that. The
// chain turns this into the usual proofread failure (raw transcript for the
// initial job, a visible error for a manual regeneration).
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
const liveNotesRevisePrompt = `你是一名会议转写校对员。输入是语音识别的会议转写，每行格式为「[mm:ss] 说话人：内容」。

你的任务只有一件：找出听写错误，并以「替换对」的形式输出。不要重写转写。

输出格式（每行一条，除此之外不要输出任何内容）：
原词 => 改词

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

不要输出转写原文，不要解释，不要加代码块。`

const liveNotesReviseQuestion = "请输出这一段转写的替换对。"

// liveNotePair is one substitution the proofreader is confident about.
type liveNotePair struct {
	From string
	To   string
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
	pairs := make([]liveNotePair, 0, len(chunks)*8)
	for i, chunk := range chunks {
		block := liveNoteReviseBlock(background, glossary, chunk)
		question := liveNotesReviseQuestion
		if len(chunks) > 1 {
			question = fmt.Sprintf("这是第 %d/%d 段。%s", i+1, len(chunks), question)
		}
		answer, err := summarize.AskAboutContextLossless(ctx, summarizer, systemPrompt, block, question, liveNotesReviseMaxTokens)
		if err != nil {
			return "", fmt.Errorf("live notes: revise chunk %d: %w", i+1, err)
		}
		chunkPairs, err := parseLiveNotePairs(answer)
		if err != nil {
			return "", fmt.Errorf("live notes: revise chunk %d: %w", i+1, err)
		}
		pairs = append(pairs, chunkPairs...)
		if len(progress) > 0 && progress[0] != nil {
			progress[0](i+1, len(chunks))
		}
	}
	return applyLiveNotePairs(transcript, pairs), nil
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

// parseLiveNotePairs reads one chunk's answer as replacement pairs.
//
// Formatting noise is tolerated — list markers, backticks, a code fence, stray
// commentary lines — because those are the same answer wearing a different
// jacket. A shape that carries no pair at all is not: see ErrLiveNoteReviseFormat.
func parseLiveNotePairs(raw string) ([]liveNotePair, error) {
	text := stripLiveNoteCodeFence(strings.TrimSpace(raw))
	if text == "" {
		return nil, fmt.Errorf("%w (empty answer)", ErrLiveNoteReviseFormat)
	}
	if strings.EqualFold(strings.TrimRight(text, "。.！!"), liveNoteReviseNoChanges) {
		return nil, nil
	}

	pairs := make([]liveNotePair, 0, 8)
	var unparsed []string
	for _, line := range strings.Split(text, "\n") {
		line = trimLiveNoteListMarker(strings.TrimSpace(line))
		if line == "" || strings.EqualFold(line, liveNoteReviseNoChanges) {
			continue
		}
		left, right, ok := strings.Cut(line, "=>")
		if !ok {
			unparsed = append(unparsed, line)
			continue
		}
		from := liveNotePairSide(left)
		to := liveNotePairSide(right)
		// A pair that cannot be applied is dropped rather than fatal: the rest
		// of the chunk's corrections are still good, and the raw text is what
		// survives for this one.
		if from == "" || to == "" || from == to {
			continue
		}
		if n := utf8.RuneCountInString(from); n < 2 || n > liveNoteRevisePairMaxRunes {
			continue
		}
		if utf8.RuneCountInString(to) > liveNoteRevisePairMaxRunes {
			continue
		}
		pairs = append(pairs, liveNotePair{From: from, To: to})
	}
	if len(pairs) == 0 {
		sample := ""
		if len(unparsed) > 0 {
			sample = truncateRunes(unparsed[0], 60)
		}
		return nil, fmt.Errorf("%w (first line: %q)", ErrLiveNoteReviseFormat, sample)
	}
	return pairs, nil
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
