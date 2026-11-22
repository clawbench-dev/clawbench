package ai

// FrameAttribution is which sub-agent or team member produced one ACP frame.
//
// It is the answer to the only three questions the shared event loop needs to
// ask about attribution, kept as one value so a single hook call covers every
// branch (content, thinking, tool call). Zero fields mean "top level".
type FrameAttribution struct {
	// MemberName is the team member that produced the frame, or "" when the
	// frame is top-level or belongs to an ordinary (non-team) sub-agent.
	MemberName string
	// MemberColor is the member's display color from the team roster, or ""
	// when the member is not in the current roster.
	MemberColor string
	// ParentToolCallID is the tool call this frame belongs under, or "" for
	// top-level content. It is what folds a sub-agent's output into its Agent
	// card instead of the main timeline.
	ParentToolCallID string
}
