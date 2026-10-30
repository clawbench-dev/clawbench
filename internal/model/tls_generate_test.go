package model

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSelfSignedCert_CoversIPsInSAN(t *testing.T) {
	dir := t.TempDir()
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.1.50")}
	dns := []string{"localhost"}

	certs, err := GenerateSelfSignedCert(dir, ips, dns)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "cert.pem"), certs.CertFile)
	assert.Equal(t, filepath.Join(dir, "key.pem"), certs.KeyFile)

	raw, err := os.ReadFile(certs.CertFile)
	require.NoError(t, err)
	block, _ := pem.Decode(raw)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// The SAN is the whole point: a certificate for an IP without IP entries is
	// rejected with a name mismatch by every browser.
	// Parsed certificates hold 4-byte IPv4 values, so compare their text form.
	sans := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}
	assert.Contains(t, sans, "192.168.1.50")
	assert.Contains(t, sans, "127.0.0.1")
	assert.Contains(t, cert.DNSNames, "localhost")
	assert.NoError(t, cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature),
		"must be self-signed (a leaf certificate cannot sign another, so check the signature directly)")
	assert.True(t, cert.NotAfter.After(cert.NotBefore))
	assert.Contains(t, cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth)

	info, err := os.Stat(certs.KeyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the private key must not be world-readable")
}

func TestGenerateSelfSignedCert_RejectsEmptyInputs(t *testing.T) {
	_, err := GenerateSelfSignedCert("", []net.IP{net.ParseIP("127.0.0.1")}, nil)
	assert.Error(t, err)

	_, err = GenerateSelfSignedCert(t.TempDir(), nil, nil)
	assert.Error(t, err)
}

func TestLocalTLSSANs_AlwaysCoversLoopback(t *testing.T) {
	ips, dns := LocalTLSSANs()
	assert.Contains(t, dns, "localhost")
	found := false
	for _, ip := range ips {
		if ip.String() == "127.0.0.1" {
			found = true
		}
	}
	assert.True(t, found, "loopback must always be covered")
}

func TestDescribeTLSSANs_SortsAndJoinsEverything(t *testing.T) {
	got := DescribeTLSSANs(
		[]net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("127.0.0.1")},
		[]string{"localhost", "clawbench.local"},
	)
	assert.Equal(t, "10.0.0.5, 127.0.0.1, clawbench.local, localhost", got)

	assert.Empty(t, DescribeTLSSANs(nil, nil))
}

// An unwritable target must be reported, not silently skipped: the caller shows
// the reason to the user.
func TestGenerateSelfSignedCert_CreateDirFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	_, err := GenerateSelfSignedCert(filepath.Join(blocker, "tls"), []net.IP{net.ParseIP("127.0.0.1")}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create cert dir")
}
