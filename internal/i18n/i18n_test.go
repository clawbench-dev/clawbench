package i18n

import (
	"net/http"
	"net/http/httptest"
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
	assert.Equal(t, "Session limit reached (50), please delete old sessions", msg)
}

func TestT_TemplateDataChinese(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("X-Locale", "zh")
	loc := Localizer(r)

	msg := T(loc, "SessionLimitReached", map[string]interface{}{"MaxCount": 50})
	assert.Equal(t, "已达会话数量上限（50），请先删除旧会话", msg)
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
		"NoAgentsAvailable",
		"MessageOrFilesRequired",
		"TextRequired",
		"JobIdRequired",
		"FileNotFound",
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
	assert.Equal(t, "AI Task Completed", msg)
}

func TestLocalizerForLocale_Chinese(t *testing.T) {
	loc := LocalizerForLocale("zh")
	msg := T(loc, "PushTaskCompleted")
	assert.Equal(t, "AI任务完成", msg)
}

func TestLocalizerForLocale_EmptyDefaultsToEnglish(t *testing.T) {
	loc := LocalizerForLocale("")
	msg := T(loc, "PushSessionEnded")
	assert.Equal(t, "AI session ended", msg)
}

func TestLocalizerForLocale_PushNotificationKeys(t *testing.T) {
	// Verify all push notification i18n keys exist in both languages
	keys := []string{"PushTaskCompleted", "PushSessionEnded", "PushScheduledTaskDone", "PushPermissionPending"}
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
