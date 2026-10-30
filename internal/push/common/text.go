package common

import (
	"clawbench/internal/i18n"
	"clawbench/internal/model"
)

// T localizes one message for the IM push backends.
//
// The push backends run on their own goroutines — an inbound IM callback or a
// terminal-event hook — with no HTTP request in scope, so they cannot use the
// per-request localizer. They read the language the frontend persists to the
// server (model.Language) instead, exactly like service.AutoContinuePrompt.
// Without this the bot wrote Chinese into every user's chat regardless of the
// UI language.
//
// Falls back to the message key when a translation is missing, matching
// i18n.T's contract, so a typo shows up as a visible key rather than an empty
// message.
func T(messageID string, templateData ...map[string]any) string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), messageID, templateData...)
}

// Template placeholders shared by several push strings. Kept as constants
// because a typo in one of these silently renders "{{.Count}}" into the chat,
// and because both backends use the same names across many call sites.
const (
	// TemplateCount is the message count placeholder (queued/files).
	TemplateCount = "Count"
	// TemplateLabel is the session label placeholder.
	TemplateLabel = "Label"
	// TemplateID is the short session id placeholder.
	TemplateID = "ID"
	// TemplateProject is the project path/label placeholder.
	TemplateProject = "Project"
	// TemplateCommand is the slash-command word placeholder.
	TemplateCommand = "Command"
)

// CommandWithAttachmentText is the reply for a slash command that arrived
// together with an attachment.
//
// Commands are not routed to a session, and the attachment path delivers
// Route.Message (which a command leaves empty) — so silently continuing would
// post an empty message and drop the command word. Both backends answer with
// this instead: it names the command and tells the user to send the files
// separately, which is the only way to get both.
func CommandWithAttachmentText(command string) string {
	return T("PushCommandWithAttachment", map[string]any{TemplateCommand: command})
}
