package model

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testCertPEM = `-----BEGIN CERTIFICATE-----
Zm9vYmFy
-----END CERTIFICATE-----
`
	testKeyPEM = `-----BEGIN PRIVATE KEY-----
YmFyYmF6
-----END PRIVATE KEY-----
`
)

// writeTLSCertDir creates a temp dir containing a valid separate cert+key pair
// (fullchain.pem + privkey.pem) and returns its path. Used by tests across the
// model and cli packages to exercise TLS resolution.
func writeTLSCertDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), []byte(testCertPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), []byte(testKeyPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolveTLSCerts_LetEncryptSeparate(t *testing.T) {
	dir := writeTLSCertDir(t)

	certs, ok := ResolveTLSCerts(dir)
	if !ok {
		t.Fatal("expected TLS to be active with fullchain.pem + privkey.pem")
	}
	if certs.CertFile != filepath.Join(dir, "fullchain.pem") {
		t.Errorf("CertFile = %s, want fullchain.pem", certs.CertFile)
	}
	if certs.KeyFile != filepath.Join(dir, "privkey.pem") {
		t.Errorf("KeyFile = %s, want privkey.pem", certs.KeyFile)
	}
}

func TestResolveTLSCerts_GenericSeparate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cert.pem"), testCertPEM)
	writeFile(t, filepath.Join(dir, "key.pem"), testKeyPEM)

	certs, ok := ResolveTLSCerts(dir)
	if !ok {
		t.Fatal("expected TLS active with cert.pem + key.pem")
	}
	if certs.CertFile != filepath.Join(dir, "cert.pem") {
		t.Errorf("CertFile = %s, want cert.pem", certs.CertFile)
	}
	if certs.KeyFile != filepath.Join(dir, "key.pem") {
		t.Errorf("KeyFile = %s, want key.pem", certs.KeyFile)
	}
}

func TestResolveTLSCerts_CombinedFile(t *testing.T) {
	dir := t.TempDir()
	combined := testCertPEM + testKeyPEM
	writeFile(t, filepath.Join(dir, "combined.pem"), combined)

	certs, ok := ResolveTLSCerts(dir)
	if !ok {
		t.Fatal("expected TLS active with combined.pem")
	}
	if certs.CertFile != certs.KeyFile {
		t.Errorf("CertFile and KeyFile should be the same combined file, got %s vs %s", certs.CertFile, certs.KeyFile)
	}
}

func TestResolveTLSCerts_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ResolveTLSCerts(dir); ok {
		t.Fatal("expected no TLS when dir is empty")
	}
}

func TestResolveTLSCerts_NonExistentDir(t *testing.T) {
	if _, ok := ResolveTLSCerts(filepath.Join(t.TempDir(), "nope")); ok {
		t.Fatal("expected no TLS for non-existent dir")
	}
}

func TestResolveTLSCerts_EmptyString(t *testing.T) {
	if _, ok := ResolveTLSCerts(""); ok {
		t.Fatal("expected no TLS for empty dir")
	}
}

func TestResolveTLSCerts_NotADirectory(t *testing.T) {
	f := t.TempDir() + "/file.txt"
	writeFile(t, f, "not a dir")
	if _, ok := ResolveTLSCerts(f); ok {
		t.Fatal("expected no TLS when path is a file")
	}
}

func TestResolveTLSCerts_MissingKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fullchain.pem"), testCertPEM)
	// no privkey.pem
	if _, ok := ResolveTLSCerts(dir); ok {
		t.Fatal("expected no TLS when key is missing")
	}
}

func TestResolveTLSCerts_CombinedNoKey(t *testing.T) {
	dir := t.TempDir()
	// combined.pem exists but contains only a cert block (no private key)
	writeFile(t, filepath.Join(dir, "combined.pem"), testCertPEM)
	if _, ok := ResolveTLSCerts(dir); ok {
		t.Fatal("expected no TLS when combined.pem lacks a private key")
	}
}

func TestResolveTLSActive_UsesDefaultDir(t *testing.T) {
	orig := DataDir
	t.Cleanup(func() { DataDir = orig })
	DataDir = t.TempDir()
	cfg := Config{}
	if cfg.ResolveTLSActive() {
		t.Fatal("expected inactive when default dir has no certs")
	}
}

func TestDefaultTLSCertDir(t *testing.T) {
	orig := DataDir
	t.Cleanup(func() { DataDir = orig })
	DataDir = "/tmp/custom-data"
	got := DefaultTLSCertDir()
	want := filepath.Join("/tmp/custom-data", "config", "tls")
	if got != want {
		t.Errorf("DefaultTLSCertDir = %s, want %s", got, want)
	}
}

func TestApplyDefaults_MigratesLegacyTLSFields(t *testing.T) {
	orig := DataDir
	t.Cleanup(func() { DataDir = orig })
	DataDir = t.TempDir()

	cfg := Config{TLS: struct {
		CertDir  string `yaml:"cert_dir"`
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	}{CertFile: "/some/dir/fullchain.pem", KeyFile: "/some/dir/privkey.pem"}}
	ApplyDefaults(&cfg, nil)

	if cfg.TLS.CertDir != "/some/dir" {
		t.Errorf("CertDir after migration = %q, want /some/dir", cfg.TLS.CertDir)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
