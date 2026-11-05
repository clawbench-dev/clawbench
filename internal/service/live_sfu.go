package service

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// LiveSFUDDL creates the relay (LiveKit) configuration table.
//
// One row, id=1: the relay is a property of this installation, not of a
// meeting. Meetings snapshot it at creation (see liveshare.Room), so editing it
// never reaches a room that is already running.
//
// The secret lives here rather than in config.yaml because config.yaml is
// copied, backed up and pasted into chat far more often than a database file is.
// The HTTP layer returns it only on an explicit `?reveal=1` — see
// handler/live_sfu.go.
const LiveSFUDDL = `
CREATE TABLE IF NOT EXISTS live_sfu_config (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	url TEXT NOT NULL DEFAULT '',
	api_key TEXT NOT NULL DEFAULT '',
	api_secret TEXT NOT NULL DEFAULT '',
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// LiveSFUConfig is the stored relay endpoint plus its credential.
//
// URL is the address a BROWSER connects to (ws:// or wss://), because that is
// the one whose scheme has to match the page's. The server-side address is
// derived: the same host over http/https, which is what LiveKit's Twirp API
// listens on.
type LiveSFUConfig struct {
	URL       string
	APIKey    string
	APISecret string
	UpdatedAt string
}

// Configured reports whether a usable endpoint + credential pair is stored.
func (c LiveSFUConfig) Configured() bool {
	return c.URL != "" && c.APIKey != "" && c.APISecret != ""
}

// APIBaseURL is the server-side address for LiveKit's HTTP/Twirp API.
//
// Derived from URL rather than stored separately: two fields that must agree
// are two fields that eventually disagree, and the only difference is the
// scheme. A ws:// endpoint is served by an http:// API.
func (c LiveSFUConfig) APIBaseURL() string {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = schemeHTTPS
	case "ws":
		u.Scheme = schemeHTTP
	default:
		return ""
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/")
}

// GetLiveSFUConfig reads the single row. A missing row is not an error: it
// means "not configured yet", which every caller has to handle anyway.
func GetLiveSFUConfig() (*LiveSFUConfig, error) {
	cfg := &LiveSFUConfig{}
	var updatedAt sql.NullString
	err := store.ReadDB().QueryRow(
		`SELECT url, api_key, api_secret, updated_at FROM live_sfu_config WHERE id = 1`,
	).Scan(&cfg.URL, &cfg.APIKey, &cfg.APISecret, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get live sfu config: %w", err)
	}
	cfg.UpdatedAt = updatedAt.String
	return cfg, nil
}

// LiveSFUPatch is one edit. A nil key/secret means "leave what is stored", which
// is what lets the UI show a masked field and save a URL change without wiping
// the credential it never received.
type LiveSFUPatch struct {
	URL       string
	APIKey    *string
	APISecret *string
	Clear     bool
}

// SaveLiveSFUConfig applies a patch and returns the stored result.
func SaveLiveSFUConfig(p LiveSFUPatch) (*LiveSFUConfig, error) {
	current, err := GetLiveSFUConfig()
	if err != nil {
		return nil, err
	}
	next := *current
	if p.Clear {
		next.APIKey = ""
		next.APISecret = ""
	} else {
		if p.APIKey != nil {
			next.APIKey = strings.TrimSpace(*p.APIKey)
		}
		if p.APISecret != nil {
			next.APISecret = strings.TrimSpace(*p.APISecret)
		}
	}
	if p.URL != "" || p.Clear {
		next.URL = strings.TrimSpace(p.URL)
	}
	if err := validateLiveSFUConfig(next); err != nil {
		return nil, err
	}
	if _, err := store.WriteExec(
		`INSERT INTO live_sfu_config (id, url, api_key, api_secret, updated_at)
		 VALUES (1, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET
		   url = excluded.url,
		   api_key = excluded.api_key,
		   api_secret = excluded.api_secret,
		   updated_at = CURRENT_TIMESTAMP`,
		next.URL, next.APIKey, next.APISecret,
	); err != nil {
		return nil, fmt.Errorf("save live sfu config: %w", err)
	}
	return GetLiveSFUConfig()
}

// ErrLiveSFUInvalid wraps every validation failure so the handler can answer 400
// with the reason instead of a generic bad-request.
var ErrLiveSFUInvalid = errors.New("invalid live sfu config")

func validateLiveSFUConfig(c LiveSFUConfig) error {
	// Clearing everything is a valid state: it means "relay not offered".
	if c.URL == "" && c.APIKey == "" && c.APISecret == "" {
		return nil
	}
	if c.URL == "" {
		return fmt.Errorf("%w: %s", ErrLiveSFUInvalid, i18n.T(i18n.LocalizerForLocale(model.Language), "LiveSFUURLRequired"))
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: %s", ErrLiveSFUInvalid, i18n.T(i18n.LocalizerForLocale(model.Language), "LiveSFUURLUnparsable"))
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("%w: %s", ErrLiveSFUInvalid, i18n.T(i18n.LocalizerForLocale(model.Language), "LiveSFUURLScheme"))
	}
	// A credential is a pair. Half of one is always a mistake — most often a
	// paste that caught only the first line — and would fail at the first
	// meeting with a 401 that says nothing about which field is wrong.
	if (c.APIKey == "") != (c.APISecret == "") {
		return fmt.Errorf("%w: %s", ErrLiveSFUInvalid, i18n.T(i18n.LocalizerForLocale(model.Language), "LiveSFUCredentialsPair"))
	}
	return nil
}
