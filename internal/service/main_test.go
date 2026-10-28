package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
)

// TestMain pins model.Language to the production default (Chinese).
//
// Several service strings are localized at WRITE time from a background
// goroutine with no HTTP request in scope — the group-timeline system rows
// (member joined/disabled/enabled, the unknown-mention notice) and the forge
// event-context block — via i18n.LocalizerForLocale(model.Language). In a bare
// test binary
// model.Language is the zero value "", which the localizer resolves to English,
// so assertions on the Chinese wording would fail even though production (which
// sets model.Language from config, defaulting to zh) renders Chinese.
//
// Tests that need another locale set model.Language themselves and restore it
// (see auto_continue_test.go), so this only fixes the baseline.
//
// It also replaces the port-activity probe's dialer with a stub. checkPortActive
// runs SYNCHRONOUSLY inside RegisterPort/UpdatePort, and the proxy tests
// register non-localhost targets (192.168.1.100) purely to exercise host
// persistence — never to assert reachability. Against a real, unreachable
// address every such call burned the full 5s non-localhost budget (~95s across
// the suite on Linux), and on Windows the OS connect can outlast even that cap,
// timing the whole package out at Go's 600s limit (observed on main, twice).
// The stub keeps those tests hermetic; tests that DO care about the probe's
// result install their own dialer with SetPortDialForTest.
func TestMain(m *testing.M) {
	model.Language = model.DefaultLanguage
	// No restore: m.Run() is the last statement and os.Exit ends the process, so
	// a deferred restore would never execute (and gocritic's exitAfterDefer
	// rightly flags it).
	SetPortDialForTest(func(addr string, timeout time.Duration) bool {
		// Loopback targets keep their real semantics (a local listener is
		// genuinely reachable); only the non-localhost dial — the one that
		// stalls — is short-circuited to "not listening".
		if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") {
			return dialPort(addr, timeout)
		}
		return false
	})
	os.Exit(m.Run())
}
