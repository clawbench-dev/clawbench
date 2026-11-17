package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"clawbench/internal/agentcore/compaction"
	"clawbench/internal/model"
)

// Agent-mode minutes and proofreading.
//
// Why this exists: the summary path (summarize.AskAboutContext) can only see the
// text it is handed. A meeting about "the config loader" therefore produces
// minutes that say "the config loader", because nothing could look up which file
// that actually is. The built-in agent can open the project, read the previous
// meeting's minutes, and grep for how a term is spelled in the code — so the
// minutes can name the real thing.
//
// What this deliberately does NOT change:
//   - chunking, language, the context block, the prompt text: those are the
//     same inputs the summary path uses, assembled by the same code. Only the
//     model call is replaced.
//   - the proofreader's `原词 => 改词` pair format: rewriting the transcript
//     directly would lose timestamps and speaker labels. The agent produces the
//     same pairs, and the same applier merges them.

// liveNotesAgentTimeout bounds one agent run per artifact.
//
// A meeting's minutes are a background job the user waits on; an agent that
// wanders the project for twenty minutes is worse than a mediocre summary now.
// Generous enough for a few file reads, short enough to fail visibly.
const liveNotesAgentTimeout = 5 * time.Minute

// agentMinutesPolicy is the tool grant for minutes generation.
//
// Read-only: producing minutes is transcription and lookup, never editing. The
// previous meeting's minutes are read, project files are read, nothing is
// written — the caller writes the artifact.
func agentMinutesPolicy(maxRounds int) model.ToolPolicy {
	return model.ToolPolicy{
		Allow:     []string{model.CapFSRead},
		MaxRounds: maxRounds,
	}
}

// agentMinutesSystemPrompt is the role instruction for the agent run.
//
// It states the tool boundary explicitly because a read-only agent that does not
// know why it cannot write will spend rounds looking for a way to.
const agentMinutesSystemPrompt = `你是会议记录员。你的任务是读会议材料，产出一份准确的会议纪要。

你可以读工作目录里的文件来核对专有名词：如果转写里提到的模块、文件名、配置项、接口名在项目中存在，用项目里的真实写法，不要照抄语音识别的结果。

不要修改任何文件——你只需要产出文本。`

// agentReviseSystemPrompt is the role instruction for agent-mode proofreading.
const agentReviseSystemPrompt = `你是会议转写的校对员。你的任务是找出转写里被语音识别听错的词，并给出替换对。

你可以读工作目录里的文件来核对专有名词的正确写法。

只输出替换对，不要改写整段文字。`

// generateLiveNotesMinutesWithAgent produces minutes through the built-in agent.
//
// The chunking and the final assembly mirror GenerateLiveNotesMinutesWithOptions
// exactly: same chunk size, same prompts, same context block, same language
// directive. Only the model call differs, so switching modes cannot silently
// change what the minutes are allowed to contain.
func generateLiveNotesMinutesWithAgent(
	ctx context.Context, title, transcript, contextSection string, self LiveNotesSpeaker,
	opts LiveNotesMinutesOptions, workDir string,
	progress ...func(done, total int, phase string),
) (string, error) {
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}
	report := func(done, total int, phase string) {
		if len(progress) > 0 && progress[0] != nil {
			progress[0](done, total, phase)
		}
	}

	maxRounds := model.ConfigInstance.Meeting.AgentMinutesMaxRounds
	policy := agentMinutesPolicy(maxRounds)

	chunks := splitRunes(transcript, liveNotesMinutesChunkRunes)
	contextBlock := transcript
	if len(chunks) > 1 {
		partials := make([]string, 0, len(chunks))
		for i, chunk := range chunks {
			prompt := fmt.Sprintf("第 %d/%d 段：\n%s\n\n请提取这一段的关键信息。", i+1, len(chunks), chunk)
			out, err := RunBuiltinAgentTask(ctx, BuiltinAgentTask{
				Prompt:          prompt,
				SystemPrompt:    agentMinutesSystemPrompt + "\n\n" + self.withPrompt(liveNotesChunkPrompt),
				WorkDir:         workDir,
				Policy:          policy,
				MaxOutputTokens: compaction.MaxSummaryTokens,
				Timeout:         liveNotesAgentTimeout,
			})
			if err != nil {
				return "", fmt.Errorf("live notes: agent chunk %d: %w", i+1, err)
			}
			partials = append(partials, out)
			report(i+1, len(chunks), "chunk")
		}
		contextBlock = strings.Join(partials, "\n\n")
	}
	// Same placement rule as the summary path: after the reduction, never
	// inside it, so a file list in one chunk's tail is not summarized away.
	if strings.TrimSpace(contextSection) != "" {
		contextBlock += "\n\n" + contextSection
	}

	report(0, 1, "final")
	systemPrompt := agentMinutesSystemPrompt + "\n\n" + self.withPrompt(liveNotesMinutesPrompt)
	if directive := liveNotesLanguageDirective(opts.Language); directive != "" {
		systemPrompt += "\n\n" + directive
	}

	minutes, err := RunBuiltinAgentTask(ctx, BuiltinAgentTask{
		Prompt:          contextBlock + "\n\n" + liveNotesMinutesQuestion(title),
		SystemPrompt:    systemPrompt,
		WorkDir:         workDir,
		Policy:          policy,
		MaxOutputTokens: opts.MaxTokens,
		Timeout:         liveNotesAgentTimeout,
	})
	if err != nil {
		return "", fmt.Errorf("live notes: agent minutes: %w", err)
	}
	if strings.TrimSpace(minutes) == "" {
		return "", fmt.Errorf("live notes: agent minutes: the agent returned nothing")
	}
	return strings.TrimSpace(minutes), nil
}

// reviseLiveNotesTranscriptWithAgent proofreads through the built-in agent.
//
// The output contract is unchanged: a list of `原词 => 改词` pairs, applied by
// applyLiveNoteEdits. The agent may READ files to check a spelling, which is the
// whole point — but it may not rewrite the transcript, because that is what
// keeps every timestamp and speaker label intact.
func reviseLiveNotesTranscriptWithAgent(
	ctx context.Context, in LiveNotesReviseInput, workDir string,
	progress ...func(done, total int),
) (string, error) {
	transcript := BuildLiveNotesTranscript(in.Segments)
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}

	maxRounds := model.ConfigInstance.Meeting.AgentMinutesMaxRounds
	policy := agentMinutesPolicy(maxRounds)

	background := strings.Join(projectContext(in.ProjectPath), "\n\n")
	glossary := LiveNoteGlossaryPromptSection(in.Title, in.Files, in.Segments, in.ProjectPath)
	systemPrompt := agentReviseSystemPrompt + "\n\n" + liveNotesRevisePrompt
	if note := in.Self.reviseNote(); note != "" {
		systemPrompt += "\n\n" + note
	}

	chunks := splitRunes(transcript, liveNotesReviseChunkRunes)
	pairs := make([]liveNotePair, 0, len(chunks)*8)
	splits := make([]liveNoteSplit, 0, 2)
	for i, chunk := range chunks {
		block := liveNoteReviseBlock(background, glossary, chunk)
		question := liveNotesReviseQuestion
		if len(chunks) > 1 {
			question = fmt.Sprintf("这是第 %d/%d 段。%s", i+1, len(chunks), question)
		}
		answer, err := RunBuiltinAgentTask(ctx, BuiltinAgentTask{
			Prompt:          block + "\n\n" + question,
			SystemPrompt:    systemPrompt,
			WorkDir:         workDir,
			Policy:          policy,
			MaxOutputTokens: liveNotesReviseMaxTokens,
			Timeout:         liveNotesAgentTimeout,
		})
		if err != nil {
			return "", fmt.Errorf("live notes: agent revise chunk %d: %w", i+1, err)
		}
		chunkPairs, chunkSplits, err := parseLiveNoteAnswer(answer)
		if err != nil {
			return "", fmt.Errorf("live notes: agent revise chunk %d: %w", i+1, err)
		}
		pairs = append(pairs, chunkPairs...)
		splits = append(splits, chunkSplits...)
		if len(progress) > 0 && progress[0] != nil {
			progress[0](i+1, len(chunks))
		}
	}
	return applyLiveNoteEdits(transcript, pairs, splits), nil
}
