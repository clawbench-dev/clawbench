package model

import (
	"encoding/pem"
	"os"
	"path/filepath"
)

// TLS cert/key filename matching rules inside a configured cert directory.
//
// Two modes are supported, checked in priority order:
//  1. Separate files (Let's Encrypt style): fullchain.pem + privkey.pem
//  2. Separate files (generic): cert.pem + key.pem
//  3. Combined single file: combined.pem (contains both cert and key PEM blocks)
//
// If any matching pair exists, HTTPS is enabled. Otherwise the server falls
// back to plain HTTP.
const (
	tlsFullchainFile = "fullchain.pem"
	tlsPrivkeyFile   = "privkey.pem"
	tlsCertFile      = "cert.pem"
	tlsKeyFile       = "key.pem"
	tlsCombinedFile  = "combined.pem"
)

// TLSCerts holds resolved certificate and key file paths for serving HTTPS.
type TLSCerts struct {
	// CertFile and KeyFile may point to the same combined PEM file.
	CertFile string
	KeyFile  string
}

// DefaultTLSCertDir returns the default directory where HTTPS cert/key files
// are expected: <DataDir>/config/tls. Returns empty string when DataDir is
// unset (not yet resolved).
func DefaultTLSCertDir() string {
	if DataDir == "" {
		return ""
	}
	return filepath.Join(DataDir, "config", "tls")
}

// ResolveTLSCerts scans dir for a valid cert/key combination per the matching
// rules documented above. Returns ok=false when no valid pair is found, meaning
// the server should fall back to HTTP.
func ResolveTLSCerts(dir string) (TLSCerts, bool) {
	if dir == "" {
		return TLSCerts{}, false
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return TLSCerts{}, false
	}

	// Rule 1: Let's Encrypt separate files (fullchain.pem + privkey.pem)
	fullchain := filepath.Join(dir, tlsFullchainFile)
	privkey := filepath.Join(dir, tlsPrivkeyFile)
	if fileExists(fullchain) && fileExists(privkey) {
		return TLSCerts{CertFile: fullchain, KeyFile: privkey}, true
	}

	// Rule 2: generic separate files (cert.pem + key.pem)
	cert := filepath.Join(dir, tlsCertFile)
	key := filepath.Join(dir, tlsKeyFile)
	if fileExists(cert) && fileExists(key) {
		return TLSCerts{CertFile: cert, KeyFile: key}, true
	}

	// Rule 3: combined single file (combined.pem) with both cert and key blocks
	combined := filepath.Join(dir, tlsCombinedFile)
	if fileExists(combined) && pemFileHasCertAndKey(combined) {
		return TLSCerts{CertFile: combined, KeyFile: combined}, true
	}

	return TLSCerts{}, false
}

// ResolveTLSActive reports whether HTTPS should be enabled based on the
// configured TLS cert directory. Uses CertDir if set, otherwise the default.
func (c *Config) ResolveTLSActive() bool {
	dir := c.TLS.CertDir
	if dir == "" {
		dir = DefaultTLSCertDir()
	}
	_, ok := ResolveTLSCerts(dir)
	return ok
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// pemFileHasCertAndKey reports whether the PEM file at path contains both at
// least one CERTIFICATE block and at least one PRIVATE KEY block, i.e. it is a
// valid combined cert+key file usable for a single-file HTTPS setup.
func pemFileHasCertAndKey(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var hasCert, hasKey bool
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch block.Type {
		case "CERTIFICATE":
			hasCert = true
		case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY":
			hasKey = true
		}
	}
	return hasCert && hasKey
}
