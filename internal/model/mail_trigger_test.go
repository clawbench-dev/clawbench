package model

import (
	"os"
	"path/filepath"
	"testing"
)

// Every known certificate file name must be removed, not just the pair the
// resolver happened to pick: leaving one behind silently turns HTTPS back on
// at the next start, which is the opposite of what "delete" promised.
func TestRemoveTLSCertsRemovesEveryKnownName(t *testing.T) {
	dir := t.TempDir()
	names := []string{tlsFullchainFile, tlsPrivkeyFile, tlsCertFile, tlsKeyFile, tlsCombinedFile}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveTLSCerts(dir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists (stat err = %v)", n, err)
		}
	}
}

func TestRemoveTLSCertsIsLenientAboutMissingFiles(t *testing.T) {
	// A directory with no certificates is the common case: the button must not
	// fail just because there was nothing to delete.
	if err := RemoveTLSCerts(t.TempDir()); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	// And an unset directory (fresh install) is a no-op.
	if err := RemoveTLSCerts(""); err != nil {
		t.Fatalf("empty dir path: %v", err)
	}
}

func TestEmailTriggerPredicates(t *testing.T) {
	email := &ScheduledTask{TriggerMode: "email"}
	if !email.IsEmailTriggered() || email.IsEventTriggered() || !email.HasEventSource() {
		t.Fatalf("email task predicates wrong: %+v", email)
	}
	event := &ScheduledTask{TriggerMode: "event"}
	if event.IsEmailTriggered() || !event.IsEventTriggered() || !event.HasEventSource() {
		t.Fatalf("event task predicates wrong: %+v", event)
	}
	cron := &ScheduledTask{TriggerMode: "cron"}
	if cron.IsEmailTriggered() || cron.IsEventTriggered() || cron.HasEventSource() {
		t.Fatalf("cron task predicates wrong: %+v", cron)
	}
}
