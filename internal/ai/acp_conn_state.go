package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// ACPConn state management — cache and emit session state
// Moved from ACPBackend (feature envy: these methods primarily operate on ACPConn data)
// ---------------------------------------------------------------------------

// sessionStateExtracted holds extracted state from an ACP session response.
// Used by CacheNewSessionState and MergeResumedSessionState to share extraction logic.
type sessionStateExtracted struct {
	modes           []ModeDef
	modeCurrentID   string
	configState     *ConfigOptionState
	efforts         []ThinkingEffortDef
	effortCurrentID string
	models          []model.AgentModel
	modelCurrentID  string
}

// CacheNewSessionState extracts and caches mode/config/thinking/model state from
// a NewSessionResponse after creating a new ACP session.
func (c *ACPConn) CacheNewSessionState() {
	sessResp := c.GetAndClearNewSessionResp()
	if sessResp == nil {
		slog.Warn("acp: CacheNewSessionState called with nil sessResp")
		return
	}
	slog.Info(
		"acp: caching new session state",
		"has_modes", sessResp.Modes != nil,
		"config_options_count", len(sessResp.ConfigOptions),
	)

	ext := c.extractSessionState(
		func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
			return sessResp, nil
		},
	)

	c.applyExtractedState(ext)
}

// MergeResumedSessionState merges state from a ResumeSessionResponse, preserving
// the user's current selections (re-applied by ensureAliveWithSession) while
// updating available options lists from the resumed agent via the registry.
func (c *ACPConn) MergeResumedSessionState() {
	resumeResp := c.GetAndClearResumeSessionResp()
	if resumeResp == nil {
		slog.Warn("acp: MergeResumedSessionState called with nil resumeResp")
		return
	}
	slog.Info(
		"acp: merging resumed session state",
		"has_modes", resumeResp.Modes != nil,
		"config_options_count", len(resumeResp.ConfigOptions),
	)

	ext := c.extractSessionState(
		func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
			return nil, resumeResp
		},
	)

	c.applyExtractedState(ext)
}

// extractSessionState extracts mode/config/thinking/model state from a session response.
// getResp returns either a NewSessionResponse or ResumeSessionResponse (one must be non-nil).
func (c *ACPConn) extractSessionState(getResp func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse)) sessionStateExtracted {
	newResp, resumeResp := getResp()
	var ext sessionStateExtracted

	// Extract mode state
	if newResp != nil {
		if modeState := extractACPModeState(newResp); modeState != nil {
			ext.modes = modeState.AvailableModes
			ext.modeCurrentID = modeState.CurrentModeID
			slog.Info("acp: extracted mode from v1 Modes field", "current", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
		} else {
			slog.Info("acp: no mode from v1 Modes field, will rely on configOptions fallback")
		}
		ext.configState = extractACPConfigOptions(newResp)
		if ext.configState != nil {
			slog.Info("acp: extracted config from configOptions", "config_id", ext.configState.ConfigID, "current", ext.configState.CurrentID, "options", len(ext.configState.Options))
		} else {
			slog.Info("acp: no mode config from configOptions")
		}
		if effortState := extractACPThinkingEffort(newResp); effortState != nil {
			ext.efforts = effortState.AvailableLevels
			ext.effortCurrentID = effortState.CurrentID
			slog.Info("acp: extracted thinking effort", "current", effortState.CurrentID, "available", len(effortState.AvailableLevels))
		} else {
			slog.Info("acp: no thinking effort from configOptions")
		}
		if modelList := extractACPModelList(newResp); modelList != nil {
			ext.models = modelList.Models
			ext.modelCurrentID = modelList.CurrentModelID
			slog.Info("acp: extracted model list", "current", modelList.CurrentModelID, "available", len(modelList.Models))
		} else if c.stdoutFilter != nil {
			if cached := c.stdoutFilter.GetAndClearCachedModels(); cached != nil {
				ext.models = cached.Models
				ext.modelCurrentID = cached.CurrentModelID
				slog.Info("acp: extracted model list from SessionModelState extension", "current", cached.CurrentModelID, "available", len(cached.Models))
			} else {
				slog.Info("acp: no model list from configOptions or SessionModelState extension")
			}
		} else {
			slog.Info("acp: no model list from configOptions")
		}
	} else {
		if modeState := extractACPModeStateFromResume(resumeResp); modeState != nil {
			ext.modes = modeState.AvailableModes
			ext.modeCurrentID = modeState.CurrentModeID
			slog.Info("acp: resumed mode state", "current", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
		} else {
			slog.Info("acp: no mode from resumed v1 Modes field")
		}
		ext.configState = extractACPConfigOptionsFromResume(resumeResp)
		if effortState := extractACPThinkingEffortFromResume(resumeResp); effortState != nil {
			ext.efforts = effortState.AvailableLevels
			ext.effortCurrentID = effortState.CurrentID
		}
		if modelList := extractACPModelListFromResume(resumeResp); modelList != nil {
			ext.models = modelList.Models
			ext.modelCurrentID = modelList.CurrentModelID
		} else if c.stdoutFilter != nil {
			if cached := c.stdoutFilter.GetAndClearCachedModels(); cached != nil {
				ext.models = cached.Models
				ext.modelCurrentID = cached.CurrentModelID
				slog.Info("acp: extracted model list from resumed SessionModelState extension", "current", cached.CurrentModelID, "available", len(cached.Models))
			}
		}
	}

	return ext
}

// applyExtractedState sets session-level current values and updates the agent-level registry.
// Always preserves user's existing selections (from PreApply) over the agent's response defaults.
func (c *ACPConn) applyExtractedState(ext sessionStateExtracted) {
	currentIDs := map[string]*string{
		"mode":          &ext.modeCurrentID,
		"thought_level": &ext.effortCurrentID,
		"model":         &ext.modelCurrentID,
	}

	// Always preserve user's existing selections over the agent's defaults.
	// This is critical for new sessions: the PreApply step in ExecuteStream
	// sets currentModeID/currentThinkingEffortID from the user's request
	// BEFORE CacheNewSessionState runs. Without this preservation,
	// the agent's reported default would overwrite the user's choice.
	for category, idPtr := range currentIDs {
		if existing := c.GetCurrentSelection(category); existing != "" {
			*idPtr = existing
		}
	}

	// Special: also update configState.CurrentID to match preserved mode
	if ext.configState != nil && ext.modeCurrentID != "" {
		ext.configState.CurrentID = ext.modeCurrentID
	}

	// Set session-level current values on ACPConn
	c.SetCurrentModeID(ext.modeCurrentID)
	c.SetCurrentThinkingEffortID(ext.effortCurrentID)
	c.SetCurrentModelID(ext.modelCurrentID)

	// Force-update agent-level registry (full overwrite, once per process instance)
	// LoadSession comes from BackendSpec (authoritative), ListSessions and
	// PromptImage from the registry — both must be carried through because
	// ForceUpdate fully replaces the AgentCapability struct.
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()
	spec := model.FindSpecByBackend(c.agent.Backend)
	loadSession := spec != nil && spec.ACPLoadSession
	listSessions := reg.GetListSessions(agentID)
	promptImage := reg.GetPromptImage(agentID)
	reg.ForceUpdateIfNeeded(agentID, ext.modes, ext.efforts, ext.models, nil, ext.configState, loadSession, listSessions, promptImage)
}

// EmitSessionStateEvents emits mode_update, thinking_effort_update, and model_list_update
// WS events. Called on every stream start (new and resumed sessions) so the frontend
// always receives the current ACP state.
func (c *ACPConn) EmitSessionStateEvents(ch chan<- StreamEvent) {
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()

	// Unified: iterate categories and build state via SelectState → domain type
	categories := []struct {
		category string
		emit     func(SelectState)
	}{
		{"mode", func(sel SelectState) {
			if ms := sel.ToModeState(); ms != nil {
				slog.Info("acp: emitting mode_update for new session", "current_mode", ms.CurrentModeID, "available", len(ms.AvailableModes))
				forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: ms})
			}
		}},
		{"thought_level", func(sel SelectState) {
			if tes := sel.ToThinkingEffortState(); tes != nil {
				slog.Debug("acp: emitting thinking_effort_update for new session", "current", tes.CurrentID, "available", len(tes.AvailableLevels))
				forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: tes})
			}
		}},
	}
	for _, cat := range categories {
		currentID := c.GetCurrentSelection(cat.category)
		if sel := reg.GetSelectState(agentID, cat.category, currentID); sel != nil && !sel.IsEmpty() {
			cat.emit(*sel)
		}
	}

	// Model list has a different structure (AgentModel with Default field), kept separate
	if modelListState := reg.GetModelListState(agentID, c.GetCurrentModelID()); modelListState != nil {
		slog.Debug("acp: emitting model_list_update for new session", "current", modelListState.CurrentModelID, "available", len(modelListState.Models))
		forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelListState})
	}
}

// EmitCommandsUpdate re-emits cached slash commands as a WS event.
func (c *ACPConn) EmitCommandsUpdate(ch chan<- StreamEvent) {
	agentID := c.AgentID()
	cmds := GetAgentCapabilityRegistry().GetCommands(agentID)
	if len(cmds) == 0 {
		if client := c.GetClient(); client != nil {
			clientCmds := client.GetCommandsAsInfo()
			if len(clientCmds) > 0 {
				cmds = clientCmds
				GetAgentCapabilityRegistry().UpdateCommands(agentID, cmds)
			}
		}
	}
	if len(cmds) == 0 {
		return
	}
	slog.Info("acp: re-emitting cached commands_update", "count", len(cmds), "source", func() string {
		if len(GetAgentCapabilityRegistry().GetCommands(agentID)) > 0 {
			return "registry"
		}
		return "client_fallback"
	}())
	forwardACPEvent(ch, StreamEvent{Type: "commands_update", Commands: cmds})
}

// ScheduleCommandsReEmit starts a timer that re-emits the commands_update event
// after the given delay. This allows time for CodeBuddy's plugin system to load
// and send an updated AvailableCommandsUpdate via ACP (issue #383).
// Returns a stop function that cancels the timer.
func (c *ACPConn) ScheduleCommandsReEmit(ch chan<- StreamEvent, delay time.Duration) func() {
	timer := time.AfterFunc(delay, func() {
		agentID := c.AgentID()
		cmds := GetAgentCapabilityRegistry().GetCommands(agentID)
		if len(cmds) == 0 {
			return
		}
		slog.Info("acp: delayed re-emitting commands_update (plugin race fix)", "agent", agentID, "count", len(cmds))
		forwardACPEvent(ch, StreamEvent{Type: "commands_update", Commands: cmds})
	})
	return func() { timer.Stop() }
}

// isACPPeerDisconnected checks whether the error is an ACP peer-disconnect error
// or a context deadline exceeded error from an ACP SDK timeout context.
// Deadline-exceeded errors from the ACP SDK are InternalError (-32603) with
// "context deadline exceeded" in the data — they indicate the agent process
// is unresponsive and should be treated the same as a disconnect for retry purposes.
func isACPPeerDisconnected(err error) bool {
	// Direct context.DeadlineExceeded (not wrapped in RequestError)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		return isPeerDisconnectMsg(err.Error()) || isACPDeadlineMsg(err.Error())
	}
	if reqErr.Code != -32603 {
		return false
	}
	if dataMap, ok := reqErr.Data.(map[string]any); ok {
		if errMsg, ok := dataMap["error"].(string); ok && (isPeerDisconnectMsg(errMsg) || isACPDeadlineMsg(errMsg)) {
			return true
		}
	}
	return isPeerDisconnectMsg(reqErr.Error()) || isACPDeadlineMsg(reqErr.Error())
}

// isPeerDisconnectMsg checks whether an error message indicates the peer
// process died or the connection pipe broke.
func isPeerDisconnectMsg(msg string) bool {
	return strings.Contains(msg, "peer disconnected") ||
		strings.Contains(msg, "broken pipe")
}

// isACPDeadlineMsg checks whether an error message indicates a context
// deadline exceeded from an ACP SDK timeout context. This happens when
// Initialize, LoadSession, ResumeSession, SetSessionConfigOption, or other
// ACP RPCs time out — the ACP SDK converts context.DeadlineExceeded into
// InternalError (-32603) with "context deadline exceeded" in the data.
func isACPDeadlineMsg(msg string) bool {
	return strings.Contains(msg, "context deadline exceeded")
}

// isUnknownConfigOption checks whether the error indicates the agent doesn't
// recognize a config option.
func isUnknownConfigOption(err error) bool {
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		return strings.Contains(err.Error(), "Unknown config option")
	}
	if dataMap, ok := reqErr.Data.(map[string]any); ok {
		if details, ok := dataMap["details"].(string); ok && strings.Contains(details, "Unknown config option") {
			return true
		}
	}
	return strings.Contains(reqErr.Error(), "Unknown config option")
}

// IsACPResourceNotFound checks whether the error indicates the ACP agent could
// not find the requested resource (specifically a session).
//
// ACP's "-32002 Resource not found" code is generic: it applies to any missing
// resource (a file, a tool, an MCP server), not just sessions. To avoid
// misreporting a load failure (e.g. a referenced file is missing) as "session
// gone", only the canonical session-scoped form is treated as a missing session:
//   - a RequestError with code -32002 whose message is "Resource not found", or
//   - a plain error whose text references the session resource directly.
func IsACPResourceNotFound(err error) bool {
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		// Plain (non-JSON-RPC) error: only treat it as session-not-found when the
		// message explicitly refers to the requested session.
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "resource not found") && strings.Contains(msg, "session")
	}
	if reqErr.Code != -32002 {
		return false
	}
	// Canonical resource-not-found: message is exactly / contains "Resource not found".
	// Avoid matching internal errors (-32603) whose data happens to embed the phrase.
	return strings.Contains(strings.ToLower(reqErr.Message), "resource not found")
}

// buildPromptBlocks constructs ACP ContentBlock list from the chat request.
// If a system prompt should be injected, it's prepended as the first text block.
// Slash commands (e.g. /reload-plugins) are sent as-is — ACP agents detect
// commands by the leading "/" and will not recognize the command if it is
// prefixed with [System Instructions: ...] or other text.
//
// Inline images are resolved from req.Images and from image paths embedded in
// the prompt's attachment tags (e.g. "[Current file: /abs/pic.png]" or
// "[User uploaded 1 file(s): .clawbench/uploads/a.png]"). Image paths are
// removed from the text prompt and appended as ContentBlock::Image blocks so
// the agent performs multimodal recognition instead of seeing a bare path.
//
// Image extraction only runs when the agent advertises the image prompt
// capability; otherwise the prompt is passed through unchanged so the user's
// file references are preserved.
func (b *ACPBackend) buildPromptBlocks(req ChatRequest) []acp.ContentBlock {
	prompt := req.Prompt

	// Prepend fork context (fork session first message) so the AI has
	// conversation history from the parent session.
	if req.ForkContext != "" {
		prompt = req.ForkContext + prompt
	}

	// Resolve inline images from the prompt's attachment tags. Skipped for
	// slash commands — they route to the agent's CommandExecutor, which does
	// not accept image content blocks. Also skipped when the agent does not
	// advertise the image prompt capability.
	var images []ImageAttachment
	imageCapable := false
	if b.agent != nil {
		imageCapable = GetAgentCapabilityRegistry().GetPromptImage(b.agent.ID)
	}
	if !IsACPSlashCommand(prompt) && imageCapable {
		prompt, images = extractImagesFromPrompt(prompt, req.WorkDir, req.Images)
	}

	// Skip system prompt injection for slash commands — ACP agents
	// detect slash commands by the leading "/" and routing depends on it.
	if req.ShouldInjectSystemPrompt() && !IsACPSlashCommand(prompt) {
		prompt = fmt.Sprintf("[System Instructions: %s]\n\n%s", req.SystemPrompt, prompt)
	}

	blocks := []acp.ContentBlock{acp.TextBlock(prompt)}

	// Append inline images. Data may already be populated (base64 passed in
	// via req.Images) or read from a local file path. The cumulative base64
	// size is budgeted so multiple images cannot overflow the agent's JSON-RPC
	// line limit (~10MB receive buffer).
	var totalBytes int
	for _, img := range images {
		est, known := estimateImageSize(img)
		if known && totalBytes+est > maxInlineImageBytesTotal {
			slog.Warn("acp: skipping inline image (cumulative size budget exceeded)",
				"path", img.Path, "cumulative_base64_bytes", totalBytes)
			continue
		}
		block, ok := buildImageBlock(img)
		if !ok {
			continue
		}
		blocks = append(blocks, block)
		if known {
			totalBytes += est
		} else {
			totalBytes += len(block.Image.Data)
		}
	}

	return blocks
}

// estimateImageSize returns the estimated base64 byte count for an image
// attachment. For Path-based images the file size is known via os.Stat; for
// Data-based images the base64 length is exact. The second return value
// reports whether the estimate is available.
func estimateImageSize(img ImageAttachment) (int, bool) {
	if img.Data != "" {
		return len(img.Data), true
	}
	if img.Path != "" {
		info, err := os.Stat(img.Path)
		if err != nil || info.IsDir() {
			return 0, false
		}
		// base64 expands raw bytes by ~4/3.
		return int((info.Size()*4 + 2) / 3), true
	}
	return 0, false
}

// imageExtensions lists file extensions treated as images for multimodal
// prompts. Extracted from the same set the frontend recognizes.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".tiff": "image/tiff",
	".tif":  "image/tiff",
	".avif": "image/avif",
	".svg":  "image/svg+xml",
}

// imageExtOf returns the image MIME type for a path, or "" if the path does
// not point to a recognized image.
func imageExtOf(path string) string {
	lower := strings.ToLower(path)
	for ext, mime := range imageExtensions {
		if strings.HasSuffix(lower, ext) {
			return mime
		}
	}
	return ""
}

// extractImagesFromPrompt scans the prompt's attachment tags for image paths,
// removes those paths from the prompt text, and returns the cleaned prompt
// together with the resolved image attachments. Images passed via `extra` are
// appended after tag-derived ones. The optional workDir is used to resolve
// relative image paths (e.g. ".clawbench/uploads/x.png").
func extractImagesFromPrompt(prompt, workDir string, extra []ImageAttachment) (string, []ImageAttachment) {
	images := make([]ImageAttachment, 0, len(extra)+4)
	seen := make(map[string]struct{})

	addImage := func(img ImageAttachment) {
		key := img.Path
		if key == "" {
			key = img.Data
		}
		if key == "" {
			return
		}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		images = append(images, img)
	}

	// Strip "[Current file: a.png, b.txt]" and "[User uploaded N file(s): ...]"
	// tags. The directory tag never carries images but shares the same shape.
	// Loop per tag name so multiple tags of the same kind (e.g. two
	// "[Current file: ...]" tags) are each processed — even when an earlier
	// tag of that kind contains no images, later ones must still be scanned.
	// Iteration stops when the tag name no longer appears in the prompt.
	clean := prompt
	for _, tag := range []string{"Current file", "User uploaded"} {
		from := 0
		for {
			next, found, changed := stripImagePathsFromTagAt(clean, tag, from, workDir, addImage)
			if !found {
				break // no more occurrences of this tag name
			}
			if changed {
				// The tag was modified/removed; rescan from the same offset
				// (text before the tag is unchanged).
				clean = next
				continue
			}
			// Tag found but unchanged (e.g. only non-images) — skip past it
			// and scan for a later same-kind tag.
			skip := strings.Index(clean[from:], "["+tag)
			if skip < 0 {
				break
			}
			end := strings.Index(clean[from+skip:], "]")
			if end < 0 {
				break
			}
			from = from + skip + end + 1
		}
	}

	for _, img := range extra {
		addImage(img)
	}

	return clean, images
}

// stripImagePathsFromTagAt rewrites the first attachment tag of tagName at or
// after offset `from`, dropping entries that resolve to image files. Matching
// entries are passed to addImage (with Path set and MimeType from the
// extension) and removed from the tag; non-image entries are preserved
// verbatim. The tag is regenerated only when at least one image entry was
// removed, so text-only prompts round-trip unchanged.
//
// Returns (newPrompt, tagFound, changed): tagFound reports whether a tag of
// tagName exists at or after `from`; changed reports whether the prompt was
// actually modified (an image entry was stripped or the whole tag removed).
func stripImagePathsFromTagAt(prompt, tagName string, from int, workDir string, addImage func(ImageAttachment)) (string, bool, bool) {
	tagPrefix := "[" + tagName
	rel := strings.Index(prompt[from:], tagPrefix)
	if rel < 0 {
		return prompt, false, false
	}
	open := from + rel
	// Find this tag's own closing bracket (the first "]" after the tag open).
	endRel := strings.Index(prompt[open:], "]")
	if endRel < 0 {
		return prompt, false, false
	}
	endIdx := open + endRel
	tag := prompt[open : endIdx+1]

	// Extract the entry list after the tag header ("N file(s): "), dropping the
	// tag's closing bracket.
	colonIdx := strings.Index(tag, ":")
	if colonIdx < 0 {
		return prompt, true, false
	}
	header := tag[:colonIdx+1]
	listStr := strings.TrimSuffix(tag[colonIdx+1:], "]")
	entries := strings.Split(listStr, ",")

	var kept []string
	for _, e := range entries {
		entry := strings.TrimSpace(e)
		if entry == "" {
			continue
		}
		resolved := resolveEntryPath(entry, workDir)
		if mime := imageExtOf(resolved); mime != "" {
			// Only strip the path when the file is actually inline-able
			// (exists, within the size cap, and contained within workDir —
			// the latter blocks arbitrary file reads via crafted prompts).
			// Otherwise keep it in the text prompt so the user's file
			// reference is preserved.
			if !imageInlineable(resolved) || !imageWithinWorkDir(resolved, workDir) {
				kept = append(kept, entry)
				continue
			}
			addImage(ImageAttachment{Path: resolved, MimeType: mime})
			continue // drop the image path from the text prompt
		}
		kept = append(kept, entry)
	}

	if len(kept) == len(entries) {
		return prompt, true, false // tag found, but no images to strip
	}

	if len(kept) == 0 {
		// All entries were images — remove the whole tag, plus a trailing
		// newline if present so we don't leave a blank line in the prompt.
		if idx := strings.Index(prompt, tag+"\n"); idx >= 0 {
			return prompt[:idx] + prompt[idx+len(tag)+1:], true, true
		}
		return strings.Replace(prompt, tag, "", 1), true, true
	}

	// Rebuild the tag with the remaining (non-image) entries, recomputing the
	// count when the header carried one.
	newTag := rebuildTag(header, kept)
	return strings.Replace(prompt, tag, newTag, 1), true, true
}

// resolveEntryPath resolves a single attachment-tag entry to a filesystem path.
// Entries may carry a line-range suffix ("path:10-20") which is stripped, and
// may be relative to workDir.
func resolveEntryPath(entry, workDir string) string {
	pathPart := entry
	if i := strings.LastIndex(entry, ":"); i > 0 {
		if isLineRange(entry[i+1:]) {
			pathPart = entry[:i]
		}
	}
	if !filepath.IsAbs(pathPart) && workDir != "" {
		return filepath.Join(workDir, pathPart)
	}
	return pathPart
}

// rebuildTag reconstructs an attachment tag header plus the kept entries,
// recomputing a "N file(s):" count when the header carried one.
func rebuildTag(header string, kept []string) string {
	newHeader := header
	if countRe := tagCountRegex; countRe.MatchString(header) {
		newHeader = countRe.ReplaceAllString(header, fmt.Sprintf("%d file(s):", len(kept)))
	}
	return newHeader + " " + strings.Join(kept, ", ") + "]"
}

// tagCountRegex matches the "N file(s):" count in an attachment tag header.
var tagCountRegex = regexp.MustCompile(`(\d+) file\(s\):`)

// imageWithinWorkDir reports whether a resolved image path stays within workDir.
// It reuses model.ValidatePath's symlink-aware containment check to prevent
// path traversal ("..") and symlink escapes from reading files outside the
// project. When workDir is empty, no path is considered contained.
func imageWithinWorkDir(resolved, workDir string) bool {
	if workDir == "" {
		return false
	}
	if filepath.IsAbs(resolved) {
		// Convert to a relative form for ValidatePath, which joins base+rel.
		rel, err := filepath.Rel(workDir, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
		resolved = rel
	}
	_, ok := model.ValidatePath(workDir, resolved)
	return ok
}

// isLineRange reports whether s looks like a strict line range used in
// file-entry labels: either a single line number ("10") or a range ("10-20",
// "10-"). Both components must be non-empty digit runs, so filenames ending in
// ":digits" (e.g. "notes:2024") are NOT mistaken for ranges.
func isLineRange(s string) bool {
	if s == "" {
		return false
	}
	// Must be all digits, optionally with a single '-' separating two digit runs.
	dashIdx := strings.IndexByte(s, '-')
	if dashIdx < 0 {
		return isAllDigits(s)
	}
	if strings.IndexByte(s[dashIdx+1:], '-') >= 0 {
		return false // more than one dash
	}
	return isAllDigits(s[:dashIdx]) && isAllDigits(s[dashIdx+1:])
}

// isAllDigits reports whether s is non-empty and contains only ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// imageInlineable reports whether a local image file is readable and within the
// inline size cap, so it can be base64-encoded into an ACP prompt. Directories
// are rejected (os.Stat on a dir returns a size, but ReadFile would fail).
func imageInlineable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Size() <= maxInlineImageBytes
}

// buildImageBlock converts an ImageAttachment into an ACP image content block.
// Data is preferred; if only Path is set, the file is read and base64-encoded
// (up to a size cap to avoid blowing up the prompt). Returns ok=false when the
// image cannot be produced (missing data, unreadable file, or oversized).
//
// When the image came from a local file, its absolute path is attached via the
// image block's uri field. CodeBuddy's ACP handler uses this path (when it is
// absolute and exists) to reference the original file instead of persisting the
// base64 payload to a temp directory and appending a synthetic path — without
// it the agent presents the same image twice (base64 block + temp-file
// reference). Note: _meta cannot be used for this because the acp-go-sdk's
// ContentBlock marshaller drops _meta on image blocks.
func buildImageBlock(img ImageAttachment) (acp.ContentBlock, bool) {
	data := img.Data
	if data == "" && img.Path != "" {
		if !imageInlineable(img.Path) {
			slog.Warn("acp: skipping inline image (unreadable or oversized)", "path", img.Path)
			return acp.ContentBlock{}, false
		}
		raw, err := os.ReadFile(img.Path)
		if err != nil {
			slog.Warn("acp: skipping inline image (read failed)", "path", img.Path, "error", err)
			return acp.ContentBlock{}, false
		}
		data = base64.StdEncoding.EncodeToString(raw)
	}
	if data == "" || img.MimeType == "" {
		return acp.ContentBlock{}, false
	}
	block := acp.ImageBlock(data, img.MimeType)

	// Attach the source path as the image URI so agents (CodeBuddy) can
	// reference the original file rather than persisting base64 to a temp dir.
	uri := img.URI
	if uri == "" && img.Path != "" {
		if abs, err := filepath.Abs(img.Path); err == nil {
			uri = abs
		}
	}
	if uri != "" {
		block.Image.Uri = &uri
	}
	return block, true
}

// maxInlineImageBytes caps how large a single image file may be inlined into an
// ACP prompt. ACP image content blocks carry base64 data (4/3 the raw size),
// and the agent's JSON-RPC receive side uses a 10MB bufio.Scanner limit on a
// single line — a 5MB image would produce ~6.7MB base64 plus envelope, which
// leaves too little headroom. 3MB keeps each image's serialized block under
// ~4MB. Beyond this limit the path label stays in the text prompt and no image
// block is emitted, so the user's file reference is preserved.
const maxInlineImageBytes = 3 << 20 // 3MB raw file → ~4MB base64

// maxInlineImageBytesTotal caps the cumulative base64 size of all inline images
// in a single prompt. Each serialized image block adds JSON-RPC envelope
// overhead, and the agent's receive side uses a 10MB bufio.Scanner line limit,
// so the sum of base64 payloads must stay well under 10MB.
const maxInlineImageBytesTotal = 8 << 20 // ~8MB cumulative base64

// IsACPSlashCommand checks if the text is an ACP slash command (e.g. /compact,
// /reload-plugins). ACP agents detect slash commands by the leading "/" and
// route them to CommandExecutor instead of the LLM. The regex matches the
// same pattern as CodeBuddy's isSlashCommand: /<letter>[<alphanumeric/hyphen>].
func IsACPSlashCommand(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 2 || t[0] != '/' {
		return false
	}
	// Must start with /<letter> followed by alphanumeric or hyphen
	c := t[1]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
