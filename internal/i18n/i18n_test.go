package i18n

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/stretchr/testify/assert"
)

func TestBundleLoaded(t *testing.T) {
	assert.NotNil(t, bundle, "bundle should be initialized")
}

func TestLocalizer_DefaultEnglish(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	loc := Localizer(r)

	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: "SessionNotRunning"})
	assert.NoError(t, err)
	assert.Equal(t, "Session is not running", msg)
}

func TestLocalizer_Chinese(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("X-Locale", "zh")
	loc := Localizer(r)

	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: "SessionNotRunning"})
	assert.NoError(t, err)
	assert.Equal(t, "会话未在运行", msg)
}

func TestLocalizer_Cookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench-locale"), Value: "zh"})
	loc := Localizer(r)

	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: "FileTooLarge"})
	assert.NoError(t, err)
	assert.Equal(t, "文件过大", msg)
}

func TestLocalizer_AcceptLanguage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	loc := Localizer(r)

	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: "FileTooLarge"})
	assert.NoError(t, err)
	assert.Equal(t, "文件过大", msg)
}

func TestLocalizer_XLocaleOverridesCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("X-Locale", "en")
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench-locale"), Value: "zh"})
	loc := Localizer(r)

	// X-Locale takes priority over cookie
	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: "FileTooLarge"})
	assert.NoError(t, err)
	assert.Equal(t, "File too large", msg)
}

func TestT_FallbackToKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	loc := Localizer(r)

	// Non-existent key should return the key itself
	msg := T(loc, "NonExistentKey")
	assert.Equal(t, "NonExistentKey", msg)
}

func TestT_TemplateData(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("X-Locale", "en")
	loc := Localizer(r)

	msg := T(loc, "SessionLimitReached", map[string]interface{}{"MaxCount": 50})
	assert.Equal(t, "Session limit reached (50), please archive old sessions", msg)
}

func TestT_TemplateDataChinese(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("X-Locale", "zh")
	loc := Localizer(r)

	msg := T(loc, "SessionLimitReached", map[string]interface{}{"MaxCount": 50})
	assert.Equal(t, "已达会话数量上限（50），请先归档旧会话", msg)
}

func TestT_AllKeysPresentInBothLanguages(t *testing.T) {
	keys := []string{
		"SessionNotRunning",
		"SessionStreamNotFound",
		"SessionStreamBusy",
		"SessionLimitReached",
		"SessionIdRequired",
		"NewSession",
		"NewSessionN",
		"ForkPrefix",
		"Session",
		"FileMessage",
		"FileTooLarge",
		"TextTooLong",
		"SummarizeFailed",
		"SynthesizeFailed",
		"BackendCreateFailed",
		"StreamStartFailed",
		"MethodNotAllowed",
		"InvalidRequestBody",
		"NotADirectory",
		"NotAFile",
		"NotAGitRepo",
		"NoProjectSelected",
		"AccessDenied",
		"Unauthorized",
		"InvalidPath",
		"PathTraversal",
		"InternalError",
		"SessionNotFound",
		"ProjectNotFound",
		"ProjectDeleteForbidden",
		"ProjectRenameForbidden",
		"ProjectPathInvalid",
		"ProjectPathExists",
		"ProjectHasRunningSessions",
		"ProjectRenameHasRunningSessions",
		"CannotDeleteCurrentProject",
		"SummaryModelNotConfigured",
		"NoUserMessagesToSummarize",
		"GenerateTitleFailed",
		"TagNameRequired",
		"TagDeleteFailed",
		"InvalidForkPoint",
		"InvalidRewindPoint",
		"NothingToRewind",
		"GroupNotRewindable",
		"ForkContextInjectedChat",
		"ForkContextInjectedGroup",
		"NoAgentsAvailable",
		"MessageOrFilesRequired",
		"TextRequired",
		"JobIdRequired",
		"FileNotFound",
		"PortAlreadyRegistered",
		// Auto-continue is rendered from a background goroutine (no request),
		// so it is the one key most likely to be added on only one side.
		"AutoContinue",
		// Restart-resume is the same no-request class: rendered from the resume
		// worker, then persisted, so a one-sided add ships a raw message id.
		// The instruction is a SEPARATE key (user-facing notice vs model prompt);
		// both must exist in both languages or one role silently gets a raw id.
		"RestartResume",
		"RestartResumeInstruction",
		"InvalidAgentAvatar",
		"AgentInUse",
		"AgentDisabled",
		"CannotDisableDefaultAgent",
		"CannotDeleteDefaultAgent",
		"AgentNotInProject",
		"CannotScopeDefaultAgent",
		"GroupYourTurn",
		"GroupDiscussionLimitReached",
		"GroupCruiseConfirming",
		// Single-chat cruise is rendered from the drain goroutine (same
		// no-request class as AutoContinue), so its three keys must be pinned
		// too or a one-sided add slips through.
		"CruiseConfirming",
		"CruiseSatisfied",
		"CruiseMaxRounds",
		"GroupCruiseSatisfied",
		"GroupMemberJoined",
		"GroupMemberDisabled",
		"GroupMemberEnabled",
		// Handler-side 409 for the disable toggle. Rendered from a request, but a
		// one-sided add (only one locale) would silently fall back to a raw key
		// in the other language, so it is pinned like the rest.
		"CannotDisableHost",
		"CannotDisableLastMember",
		"GroupMemberUnknown",
		"GroupMemberListSeparator",
		"GroupSpecialtySuffix",
		"GroupHostTitle",
		"GroupFreeTitle",
		"ACPSessionRecoveredNew",
		"ACPAuthRequired",
		"ACPAuthRequiredGeneric",
		"EventContextHeader",
		"EventContextSeparator",
		"EventContextYes",
		"EventContextNo",
		"ForgeEventItemIssue",
		"ForgeEventItemChangeRequest",
		"ForgeEventItemPipeline",
		"ForgeEventOpened",
		"ForgeEventClosed",
		"ForgeEventMerged",
		"ForgeEventReopened",
		"ForgeEventCommented",
		"ForgeEventPipelineDone",
		"ForgeEventLabelRepo",
		"ForgeEventLabelEvent",
		"ForgeEventLabelAuthor",
		"ForgeEventViewDetail",
		// The forge master switch refuses bindings while it is off.
		"ForgeDisabled",
		// Usage-meter rule rejections. These are the entire user-visible
		// explanation for a rejected settings save, so a key added on only one
		// side would ship the raw message id to that locale.
		"UsageMeterQuotaNegative",
		"UsageMeterQuotaNotNumber",
		"UsageMeterEnabledNotBool",
		"UsageMeterAnchorInvalid",
		"UsageMeterAnchorNotString",
		"UsageMeterPeriodRange",
		"UsageMeterPeriodNotNumber",
		"UsageMeterMetricInvalid",
		"UsageMeterMetricNotString",
		"UsageMeterCycleModeInvalid",
		"PushShowTitle",
		"PushShowEmpty",
		"PushShowSessionLabel",
		"PushStoppedTitle",
		"PushStoppedRunning",
		"PushStoppedIdle",
		"PushNewTitle",
		"PushNewCreated",
		"PushNewListIntro",
		"PushNewListUsage",
		"PushNewListTruncated",
		"PushNewNoProjects",
		"PushNewProjectNotFound",
		"PushNewProjectOutOfRange",
		"PushNewProjectMissingDir",
		"PushNewNoAgent",
		"PushNewListFailed",
		"PushNewCreateFailed",
		"PushStopFailed",
		"PushCommandWithAttachment",
		"PushInvalidSessionID",
		"PushShowRunning",
	}

	for _, key := range keys {
		enR := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		enR.Header.Set("X-Locale", "en")
		enLoc := Localizer(enR)
		enMsg := T(enLoc, key)
		assert.NotEqual(t, key, enMsg, "English translation missing for key: %s", key)

		zhR := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		zhR.Header.Set("X-Locale", "zh")
		zhLoc := Localizer(zhR)
		zhMsg := T(zhLoc, key)
		assert.NotEqual(t, key, zhMsg, "Chinese translation missing for key: %s", key)

		// English and Chinese should be different (unless intentionally same)
		t.Logf("%s: en=%q zh=%q", key, enMsg, zhMsg)
	}
}

// The anchor accepts a date OR a date with a time of day, and the rejection copy
// is the only thing a user sees when the value is refused. It used to name only
// YYYY-MM-DD, so a user who typed "2026-10-05 14:30" (space separator, rejected)
// was told to supply a date — following that advice would silently drop the time
// they were trying to set.
func TestUsageMeterAnchorRejectionNamesBothAcceptedShapes(t *testing.T) {
	en := func(key string) string {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Header.Set("X-Locale", "en")
		return T(Localizer(r), key)
	}
	zh := func(key string) string {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Header.Set("X-Locale", "zh")
		return T(Localizer(r), key)
	}

	for _, key := range []string{"UsageMeterAnchorInvalid", "UsageMeterAnchorNotString"} {
		// Both shapes must be visible: the date-only form AND the one with a time.
		assert.Contains(t, en(key), "YYYY-MM-DD", "%s must name the date form", key)
		assert.Contains(t, en(key), "HH:MM", "%s must also name the time form", key)
		assert.Contains(t, zh(key), "YYYY-MM-DD", "%s must name the date form", key)
		assert.Contains(t, zh(key), "HH:MM", "%s must also name the time form", key)
	}

	// The field is called "start time" now that it carries a clock, matching the
	// settings row's own label; "start date" would contradict the input.
	assert.NotContains(t, en("UsageMeterAnchorInvalid"), "start date")
	assert.NotContains(t, zh("UsageMeterAnchorInvalid"), "起始日期")
}

func TestT_NewSessionN(t *testing.T) {
	enR := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	enR.Header.Set("X-Locale", "en")
	enLoc := Localizer(enR)
	assert.Equal(t, "New Session 3", T(enLoc, "NewSessionN", map[string]interface{}{"N": 3}))

	zhR := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	zhR.Header.Set("X-Locale", "zh")
	zhLoc := Localizer(zhR)
	assert.Equal(t, "新会话 3", T(zhLoc, "NewSessionN", map[string]interface{}{"N": 3}))
}

// ---------- LocalizerForLocale (ISS-129) ----------

func TestLocalizerForLocale_English(t *testing.T) {
	loc := LocalizerForLocale("en")
	msg := T(loc, "PushTaskCompleted")
	assert.Equal(t, "Task completed", msg)
}

func TestLocalizerForLocale_Chinese(t *testing.T) {
	loc := LocalizerForLocale("zh")
	msg := T(loc, "PushTaskCompleted")
	assert.Equal(t, "任务已完成", msg)
}

func TestLocalizerForLocale_EmptyDefaultsToEnglish(t *testing.T) {
	loc := LocalizerForLocale("")
	msg := T(loc, "PushSessionCompleted")
	assert.Equal(t, "Session completed", msg)
}

func TestLocalizerForLocale_PushNotificationKeys(t *testing.T) {
	// Verify all push notification i18n keys exist in both languages.
	//
	// Two groups: the terminal-event titles (used by the dingtalk/feishu push
	// backends) and the IM reply strings the bot writes into the chat. Both run
	// on goroutines with no request context, so they localize via
	// LocalizerForLocale(model.Language) — a key added on only one side would
	// silently ship as the raw key in the other locale.
	keys := []string{
		"PushTaskCompleted", "PushTaskFailed", "PushTaskCancelled", "PushTaskStarted",
		"PushSessionCompleted", "PushSessionCancelled", "PushActionRequired",
		"PushLabelSession", "PushLabelProject", "PushLabelTask", "PushLabelAction",
		"PushLabelCommand", "PushLabelFile",
		"PushReplyHintSend", "PushReplyHintQueue",
		"PushSessionServiceUnavailable", "PushSessionUnavailable",
		"PushSendFileFailed", "PushFileSent", "PushFileTooLarge",
		"PushFilesDownloadFailed", "PushFileDownloadFailed",
		"PushSentToSessionBold", "PushSentToSessionPlain", "PushFilesCount",
		"PushPartialDownloadFailed", "PushAttachmentsTruncated",
		"PushUnsupportedMsgType", "PushSendMessageFailed", "PushMessageSent",
		"PushNoSessionSelected", "PushErrorTitle", "PushUnsupportedTitle",
		"PushSelectSessionTitle", "PushSubscribedTitle", "PushSessionListTitle",
		"PushSessionListIntro", "PushSessionListIntroFeishu", "PushNoProject", "PushNoTitle", "PushSessionLabelPrefix",
		"PushSubscribedNoAvailableDingtalk", "PushSubscribedNoAvailableFeishu",
		"PushSubscribedListFailedDingtalk", "PushSubscribedListFailedFeishu",
		"PushSubscribedNoSessionsDingtalk", "PushSubscribedNoSessionsFeishu",
		"PushSessionListIntroWechat",
		"PushSubscribedNoAvailableWechat", "PushSubscribedListFailedWechat",
		"PushSubscribedNoSessionsWechat", "PushWeChatNotLoggedIn",
		"PushQuoteLabel", "PushQuoteMedia",
		"PushMultipleRunningSessions", "PushMultipleSessions",
		"PushSessionNotFound", "PushRecentSessionUnavailable",
	}
	for _, key := range keys {
		enLoc := LocalizerForLocale("en")
		enMsg := T(enLoc, key)
		assert.NotEqual(t, key, enMsg, "English translation missing for push key: %s", key)

		zhLoc := LocalizerForLocale("zh")
		zhMsg := T(zhLoc, key)
		assert.NotEqual(t, key, zhMsg, "Chinese translation missing for push key: %s", key)

		t.Logf("%s: en=%q zh=%q", key, enMsg, zhMsg)
	}
}

// The push strings that interpolate must keep their placeholders in BOTH
// locales — a translator dropping one ships a message with a hole in it.
func TestPushTemplatesKeepPlaceholders(t *testing.T) {
	withData := map[string]map[string]any{
		"PushReplyHintSend":            {"ID": "abc"},
		"PushReplyHintQueue":           {"ID": "abc"},
		"PushFileTooLarge":             {"MaxMB": 1},
		"PushFilesDownloadFailed":      {"Count": 2},
		"PushSentToSessionBold":        {"Label": "L"},
		"PushSentToSessionPlain":       {"Label": "L"},
		"PushFilesCount":               {"Count": 2},
		"PushPartialDownloadFailed":    {"Count": 2},
		"PushAttachmentsTruncated":     {"Max": 5, "Sent": 2},
		"PushUnsupportedMsgType":       {"Type": "audio"},
		"PushMultipleRunningSessions":  {"ID": "abc"},
		"PushMultipleSessions":         {"ID": "abc"},
		"PushSessionNotFound":          {"ID": "abc"},
		"PushRecentSessionUnavailable": {"Error": "boom"},
		// Reached from the ACP connection-error path (background goroutine, no
		// request to fall back on). A missing placeholder here would ship
		// "Run `` in a terminal" to the user, so it is pinned with the rest.
		"ACPAuthRequired":           {"Command": "cmdc login"},
		"PushShowSessionLabel":      {"Label": "L"},
		"PushStoppedRunning":        {"Label": "L", "Count": 2},
		"PushStoppedIdle":           {"Label": "L", "Count": 0},
		"PushNewCreated":            {"Project": "/p", "ID": "abc"},
		"PushNewProjectOutOfRange":  {"Count": 3},
		"PushNewListTruncated":      {"Count": 2, "Total": 9},
		"PushCommandWithAttachment": {"Command": "/new"},
		"PushInvalidSessionID":      {"ID": "abc"},
	}
	for key, data := range withData {
		for _, loc := range []string{"en", "zh"} {
			msg := T(LocalizerForLocale(loc), key, data)
			assert.NotContains(t, msg, "{{", "%s locale %s left a placeholder unrendered: %q", key, loc, msg)
			assert.NotContains(t, msg, "}}", "%s locale %s left a placeholder unrendered: %q", key, loc, msg)
		}
	}
}

// Model-switch keys are reached from a background-free request path, but they
// are the entire user-visible feedback for a failed model switch — a raw key or
// a dropped placeholder would ship silently. The generic list above is a
// hand-maintained allowlist that does not include them, so they are pinned here.
func TestModelSwitchKeysPresentAndRenderable(t *testing.T) {
	require := func(cond bool, format string, args ...any) {
		if !cond {
			t.Errorf(format, args...)
		}
	}

	for _, loc := range []string{"en", "zh"} {
		l := LocalizerForLocale(loc)

		bare := T(l, "ModelSwitchFailed")
		require(bare != "ModelSwitchFailed", "%s: ModelSwitchFailed missing", loc)
		require(!strings.Contains(bare, "{{"), "%s: bare key must not carry a placeholder: %q", loc, bare)

		withDetail := T(l, "ModelSwitchFailedDetail", map[string]any{"Detail": "provider X is not connected"})
		require(withDetail != "ModelSwitchFailedDetail", "%s: ModelSwitchFailedDetail missing", loc)
		require(strings.Contains(withDetail, "provider X is not connected"),
			"%s: detail must be interpolated, got %q", loc, withDetail)
	}
}
