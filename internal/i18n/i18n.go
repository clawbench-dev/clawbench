package i18n

import (
	"embed"
	"net/http"

	"clawbench/internal/model"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localeFS embed.FS

var bundle *i18n.Bundle

func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	mustLoadEmbed("locales/active.en.yaml")
	mustLoadEmbed("locales/active.zh.yaml")
}

func mustLoadEmbed(path string) {
	data, err := localeFS.ReadFile(path)
	if err != nil {
		panic(err)
	}
	if _, err := bundle.ParseMessageFileBytes(data, path); err != nil {
		panic(err)
	}
}

// Localizer returns a Localizer for the given HTTP request.
// Language priority: X-Locale header > clawbench-locale cookie > Accept-Language header
func Localizer(r *http.Request) *i18n.Localizer {
	xLocale := r.Header.Get("X-Locale")
	cookieLocale := ""
	if c, err := r.Cookie(model.ScopedCookieName("clawbench-locale")); err == nil {
		cookieLocale = c.Value
	}
	acceptLang := r.Header.Get("Accept-Language")
	return i18n.NewLocalizer(bundle, xLocale, cookieLocale, acceptLang)
}

// T is a shorthand for localizing a message with optional template data.
// Falls back to messageID if translation is not found.
func T(loc *i18n.Localizer, messageID string, templateData ...map[string]interface{}) string {
	cfg := &i18n.LocalizeConfig{MessageID: messageID}
	if len(templateData) > 0 {
		cfg.TemplateData = templateData[0]
	}
	msg, err := loc.Localize(cfg)
	if err != nil {
		return messageID
	}
	return msg
}

// LocalizerForLocale creates a Localizer for a given locale string (e.g., "zh", "en").
// Used in contexts without an HTTP request (e.g., push notifications).
func LocalizerForLocale(locale string) *i18n.Localizer {
	return i18n.NewLocalizer(bundle, locale)
}
