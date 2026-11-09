package service

import (
	"archive/zip"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
)

func withMailDataDir(t *testing.T) string {
	t.Helper()
	orig := model.DataDir
	dir := t.TempDir()
	model.DataDir = dir
	t.Cleanup(func() { model.DataDir = orig })
	return dir
}

func TestStageMailAttachment_FileIsUsedInPlace(t *testing.T) {
	withMailDataDir(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "报告.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	atts, err := StageMailAttachments([]string{path})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("expected one attachment, got %d", len(atts))
	}
	// A plain file is referenced, not copied: the attachment path is the file
	// itself, so edits made after staging are what gets mailed.
	if atts[0].Path != path {
		t.Fatalf("attachment path = %q, want the source file %q", atts[0].Path, path)
	}
	if atts[0].Filename != "报告.txt" {
		t.Fatalf("filename = %q", atts[0].Filename)
	}
	if atts[0].Size != 5 {
		t.Fatalf("size = %d, want 5", atts[0].Size)
	}
}

func TestStageMailAttachment_MissingPath(t *testing.T) {
	withMailDataDir(t)
	_, err := StageMailAttachments([]string{filepath.Join(t.TempDir(), "nope.txt")})
	if !errors.Is(err, ErrMailAttachmentMissing) {
		t.Fatalf("err = %v, want ErrMailAttachmentMissing", err)
	}
}

func TestStageMailAttachment_OversizedFile(t *testing.T) {
	withMailDataDir(t)
	path := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(mailMaxAttachmentBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	_, err = StageMailAttachments([]string{path})
	if !errors.Is(err, ErrMailAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrMailAttachmentTooLarge", err)
	}
}

func TestStageMailAttachment_DirectoryBecomesOneZip(t *testing.T) {
	dataDir := withMailDataDir(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "素材")
	if err := os.MkdirAll(filepath.Join(dir, "子目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "子目录", "b.txt"), []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}

	atts, err := StageMailAttachments([]string{dir})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("expected one attachment, got %d", len(atts))
	}
	att := atts[0]
	if att.Filename != "素材.zip" {
		t.Fatalf("filename = %q, want 素材.zip", att.Filename)
	}
	// The zip lives under the data dir, never inside the user's tree.
	outbox := filepath.Join(dataDir, "mail", "outbox")
	if filepath.Dir(att.Path) != outbox {
		t.Fatalf("staged at %q, want a file in %q", att.Path, outbox)
	}
	info, err := os.Stat(att.Path)
	if err != nil {
		t.Fatalf("staged zip missing: %v", err)
	}
	if att.Size != info.Size() || att.Size == 0 {
		t.Fatalf("reported size %d, file size %d", att.Size, info.Size())
	}

	zr, err := zip.OpenReader(att.Path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	joined := strings.Join(names, "\n")
	// The directory itself is the top-level entry, so unzipping gives the user
	// the folder back rather than loose files.
	for _, want := range []string{"素材/", "素材/a.txt", "素材/子目录/", "素材/子目录/b.txt"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("zip is missing %q; entries:\n%s", want, joined)
		}
	}
}

func TestStageMailAttachment_OversizedDirectoryLeavesNoZip(t *testing.T) {
	dataDir := withMailDataDir(t)
	dir := filepath.Join(t.TempDir(), "huge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Random bytes are incompressible, so the archive really does grow past
	// the cap instead of deflating down to a few KB.
	blob := make([]byte, mailMaxAttachmentBytes/2)
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		name := fmt.Sprintf("part%d", i)
		if err := os.WriteFile(filepath.Join(dir, name), blob, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := StageMailAttachments([]string{dir})
	if !errors.Is(err, ErrMailAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrMailAttachmentTooLarge", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dataDir, "mail", "outbox"))
	if len(entries) != 0 {
		t.Fatalf("an oversized archive left %d files behind", len(entries))
	}
}

func TestStageMailAttachment_SkipsSymlinks(t *testing.T) {
	withMailDataDir(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	atts, err := StageMailAttachments([]string{dir})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	zr, err := zip.OpenReader(atts[0].Path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if strings.Contains(f.Name, "link.txt") {
			t.Fatalf("symlink was followed into the archive: %s", f.Name)
		}
	}
}

func TestCleanupStagedMailAttachments_OnlyRemovesOutboxFiles(t *testing.T) {
	dataDir := withMailDataDir(t)
	outbox := filepath.Join(dataDir, "mail", "outbox")
	if err := os.MkdirAll(outbox, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(outbox, "pack.zip")
	if err := os.WriteFile(staged, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(userFile, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupStagedMailAttachments([]MailOutgoingAttachment{
		{Filename: "pack.zip", Path: staged},
		{Filename: "keep.txt", Path: userFile},
	})

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged zip should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("the user's own file must survive the send: %v", err)
	}
}

func TestSweepMailOutbox_RemovesOnlyStaleArchives(t *testing.T) {
	dataDir := withMailDataDir(t)
	outbox := filepath.Join(dataDir, "mail", "outbox")
	if err := os.MkdirAll(outbox, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(outbox, "old.zip")
	fresh := filepath.Join(outbox, "new.zip")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("zip"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-mailOutboxMaxAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	sweepMailOutbox()

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("abandoned archive should be swept, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a fresh archive must survive the sweep: %v", err)
	}
}
