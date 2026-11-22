package ai

import (
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// Backend-neutral ACP tool_call parser builders
// ---------------------------------------------------------------------------
//
// Every backend's tool_call mapping is the same sequence of steps: resolve the
// name, normalize the input, and (on an update) read the status and output.
// Backends differ only in WHERE the name comes from and HOW the input is
// normalized, so the sequence lives here and a sub-package declares the one or
// two functions it actually owns.
//
// Two shapes cover every backend that registers a parser today:
//
//   - NewMetaToolCallParser: the name rides in _meta (claude, codebuddy).
//   - NewNormalizingToolCallParser: the name is encoded in the title and the
//     input needs backend-specific normalization (pi, openclaw).
//
// A backend that fits neither (codex's sub-agent frames) keeps its own parser.

// MetaToolNameFn extracts a backend's tool name from a _meta payload, or ""
// when this frame carries none.
type MetaToolNameFn func(meta map[string]any) string

// NewMetaToolCallParser builds the tool_call and tool_call_update parsers for a
// backend that names its tools in _meta. Pass a nil nameFn for a backend with
// no _meta name source at all (opencode): the name then comes from the title and
// kind exactly as it would from MapToolCallName.
//
// Input normalization uses the backend's registered InputRemaps (falling back
// to GenericACPRemaps), so the parser carries no table of its own — that is what
// keeps this shape reusable without each caller duplicating a remap copy.
func NewMetaToolCallParser(backendID string, nameFn MetaToolNameFn) (
	func(acp.SessionUpdateToolCall) *ToolCall,
	func(acp.SessionToolCallUpdate) *ToolCall,
) {
	metaName := func(meta map[string]any) string {
		if nameFn == nil {
			return ""
		}
		return nameFn(meta)
	}

	parse := func(tc acp.SessionUpdateToolCall) *ToolCall {
		name := metaName(MetaAny(tc.Meta))
		if name == "" {
			name = ExtractToolName(tc.Title, tc.Kind, backendID, string(tc.ToolCallId))
		}

		tool := &ToolCall{
			Name: name,
			ID:   string(tc.ToolCallId),
			Done: false,
		}
		ResolveACPToolInput(tc, tool, acpRemapsForBackend(backendID))
		return tool
	}

	parseUpdate := func(tcu acp.SessionToolCallUpdate) *ToolCall {
		tool := &ToolCall{ID: string(tcu.ToolCallId)}
		MapToolCallStatus(tcu.Status.Ptr(), tool)

		name := metaName(PatchMetaAny(tcu.Meta))
		if name != "" {
			tool.Name = name
		} else if title, ok := tcu.Title.Value(); ok && title != "" && !tool.Done {
			// Don't overwrite an already-correct canonical name.
			if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
				kind := acp.ToolKindExecute
				if k, ok := tcu.Kind.Value(); ok {
					kind = k
				}
				tool.Name = ExtractToolName(title, kind, backendID, string(tcu.ToolCallId))
			}
		}

		MapToolCallInput(tcu, tool, backendID)
		if tool.Done {
			MapToolCallOutput(tcu, tool)
		}
		return tool
	}

	return parse, parseUpdate
}

// ACPToolNameFromTitle returns the bare tool name a backend encodes at the
// start of a tool-call title, or "" when the title does not carry one (the
// normalizer then applies only its base remaps, which is the safe default).
type ACPToolNameFromTitle func(title string) string

// ACPToolInputNormalizer renders a backend's rawInput into the canonical JSON
// string the frontend reads. toolName is the value ACPToolNameFromTitle
// produced, so a normalizer can branch per tool (pi's nested edits[], openclaw's
// ls exception).
type ACPToolInputNormalizer func(toolName string, rawInput []byte) string

// NewNormalizingToolCallParser builds the tool_call and tool_call_update parsers
// for a backend whose rawInput needs more than a flat field remap: the nested
// edits[] array (pi, openclaw) or a per-tool exception (openclaw's ls).
func NewNormalizingToolCallParser(
	backendID string,
	nameFromTitle ACPToolNameFromTitle,
	normalize ACPToolInputNormalizer,
) (
	func(acp.SessionUpdateToolCall) *ToolCall,
	func(acp.SessionToolCallUpdate) *ToolCall,
) {
	parse := func(tc acp.SessionUpdateToolCall) *ToolCall {
		tool := &ToolCall{
			Name: ExtractToolName(tc.Title, tc.Kind, backendID, string(tc.ToolCallId)),
			ID:   string(tc.ToolCallId),
			Done: false,
		}
		if tc.RawInput != nil {
			if inputBytes, err := json.Marshal(tc.RawInput); err == nil {
				tool.Input = normalize(nameFromTitle(tc.Title), inputBytes)
			}
		}
		return tool
	}

	parseUpdate := func(tcu acp.SessionToolCallUpdate) *ToolCall {
		tool := &ToolCall{ID: string(tcu.ToolCallId)}
		MapToolCallStatus(tcu.Status.Ptr(), tool)
		if rawInput, ok := tcu.RawInput.Value(); ok {
			if inputBytes, err := json.Marshal(rawInput); err == nil {
				title, _ := tcu.Title.Value()
				tool.Input = normalize(nameFromTitle(title), inputBytes)
			}
		} else {
			MapToolCallInput(tcu, tool, backendID)
		}
		MapToolCallName(tcu, tool, backendID)
		if tool.Done {
			MapToolCallOutput(tcu, tool)
		}
		return tool
	}

	return parse, parseUpdate
}
