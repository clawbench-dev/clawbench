package handler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useTempCertDir points the handler at a scratch directory for one test.
func useTempCertDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := model.ConfigInstance.TLS.CertDir
	model.ConfigInstance.TLS.CertDir = dir
	t.Cleanup(func() { model.ConfigInstance.TLS.CertDir = prev })
	return dir
}

func TestServeTLSStatus_ReportsMissingCertificate(t *testing.T) {
	dir := useTempCertDir(t)

	w := httptest.NewRecorder()
	ServeTLSStatus(w, newRequest(t, http.MethodGet, "/api/config/tls", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var resp tlsStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Active)
	assert.False(t, resp.SelfSigned)
	assert.Equal(t, dir, resp.CertDir)
}

func TestServeTLSGenerate_WritesSelfSignedAndRequiresRestart(t *testing.T) {
	dir := useTempCertDir(t)

	w := httptest.NewRecorder()
	ServeTLSGenerateSelfSigned(w, newRequest(t, http.MethodPost, "/api/config/tls/self-signed", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tlsStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Active)
	assert.True(t, resp.SelfSigned)
	assert.True(t, resp.RestartRequired)
	// The machine's own addresses are always covered, otherwise the link a
	// colleague opens fails with a name mismatch.
	assert.Contains(t, resp.Hosts, "127.0.0.1")
	assert.Contains(t, resp.Hosts, "localhost")
	assert.NotEmpty(t, resp.NotAfter)

	_, err := os.Stat(filepath.Join(dir, "cert.pem"))
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(dir, "key.pem"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestServeTLSGenerate_AddsExtraHosts(t *testing.T) {
	useTempCertDir(t)

	w := httptest.NewRecorder()
	ServeTLSGenerateSelfSigned(w, newRequest(t, http.MethodPost, "/api/config/tls/self-signed", map[string]any{
		"hosts": []string{"10.9.8.7", "clawbench.local"},
	}))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tlsStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Hosts, "10.9.8.7")
	assert.Contains(t, resp.Hosts, "clawbench.local")
}

// A CA-issued certificate cannot be regenerated here, so overwriting one must
// take an explicit `force`.
func TestServeTLSGenerate_RefusesToOverwriteCAIssuedCert(t *testing.T) {
	dir := useTempCertDir(t)
	writeCAIssuedCert(t, dir)

	w := httptest.NewRecorder()
	ServeTLSGenerateSelfSigned(w, newRequest(t, http.MethodPost, "/api/config/tls/self-signed", nil))
	assert.Equal(t, http.StatusConflict, w.Code)

	// Still the original file.
	assert.False(t, isSelfSigned(filepath.Join(dir, "cert.pem")))

	w = httptest.NewRecorder()
	ServeTLSGenerateSelfSigned(w, newRequest(t, http.MethodPost, "/api/config/tls/self-signed", map[string]any{
		"force": true,
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, isSelfSigned(filepath.Join(dir, "cert.pem")))
}

// writeCAIssuedCert writes a leaf whose issuer differs from its subject, i.e.
// exactly what a real CA-signed certificate looks like.
func writeCAIssuedCert(t *testing.T, dir string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ClawBench"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cert.pem"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		0o644,
	))
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "key.pem"),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		0o600,
	))
}
