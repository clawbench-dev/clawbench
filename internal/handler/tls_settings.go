// HTTPS certificate settings: status + one-click self-signed generation.
//
// The CLI (`clawbench --gen-cert`) came first because it was the shortest path
// to a working HTTPS listener. But the people who need it are the ones who
// never open a terminal — and the certificate has to be regenerated whenever
// the machine's address changes, which is exactly when nobody is at a shell.
// So the same generator is exposed here and driven from the settings panel.
package handler

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawbench/internal/model"
)

// tlsStatusResponse is what both endpoints return: enough for the panel to
// render the current state without a second round trip.
type tlsStatusResponse struct {
	// Active means a usable cert/key pair exists on disk (HTTPS is served on the
	// next start; the running process keeps whatever it started with).
	Active bool `json:"active"`
	// Serving is whether THIS request arrived over TLS.
	Serving bool `json:"serving"`
	// SelfSigned is true when the certificate signs itself, i.e. the browser
	// will show an "untrusted" warning until the user accepts it once.
	SelfSigned bool     `json:"selfSigned"`
	CertFile   string   `json:"certFile,omitempty"`
	KeyFile    string   `json:"keyFile,omitempty"`
	CertDir    string   `json:"certDir"`
	Hosts      []string `json:"hosts,omitempty"`
	NotAfter   string   `json:"notAfter,omitempty"`
	// HTTPSURL is the address this server would be reachable at over TLS,
	// derived from the current request. Empty when the port is unknown.
	HTTPSURL string `json:"httpsUrl,omitempty"`
	// RestartRequired is set by the generate endpoint: the listener is created
	// at startup, so a new certificate only takes effect after a restart.
	RestartRequired bool `json:"restartRequired,omitempty"`
}

// ServeTLSStatus handles GET /api/config/tls.
func ServeTLSStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, buildTLSStatus(r))
}

// ServeTLSGenerateSelfSigned handles POST /api/config/tls/self-signed.
//
// Body (optional): {"hosts": ["10.0.0.5", "clawbench.local"], "force": false}
//
// The machine's current addresses are always included; `hosts` adds any that
// are not currently assigned (a fixed office IP, a DNS name). `force` is
// required to overwrite a certificate that is NOT self-signed — those come from
// a real CA, cannot be regenerated here, and losing them would take the site
// offline.
func ServeTLSGenerateSelfSigned(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Hosts []string `json:"hosts"`
		Force bool     `json:"force"`
	}
	// An empty body is fine: it means "cover whatever this machine has now".
	if r.ContentLength > 0 && !decodeJSON(w, r, &body) {
		return
	}

	dir := tlsCertDir()
	if existing, ok := model.ResolveTLSCerts(dir); ok {
		if !isSelfSigned(existing.CertFile) && !body.Force {
			writeLocalizedErrorf(w, r, http.StatusConflict, "TLSCertNotSelfSigned")
			return
		}
	}

	ips, dnsNames := model.LocalTLSSANs()
	for _, raw := range body.Hosts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if ip := net.ParseIP(raw); ip != nil {
			ips = append(ips, ip)
			continue
		}
		dnsNames = append(dnsNames, raw)
	}

	if _, err := model.GenerateSelfSignedCert(dir, ips, dnsNames); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "TLSCertGenerateFailed",
			map[string]any{"Error": err.Error()})
		return
	}

	status := buildTLSStatus(r)
	status.RestartRequired = true
	writeJSON(w, http.StatusOK, status)
}

// tlsCertDir resolves the configured certificate directory, falling back to the
// default under the data directory.
func tlsCertDir() string {
	if dir := model.ConfigInstance.TLS.CertDir; dir != "" {
		return dir
	}
	return model.DefaultTLSCertDir()
}

// buildTLSStatus inspects the certificate on disk.
func buildTLSStatus(r *http.Request) tlsStatusResponse {
	dir := tlsCertDir()
	resp := tlsStatusResponse{
		CertDir:  dir,
		Serving:  r.TLS != nil,
		HTTPSURL: httpsURLFor(r),
	}
	certs, ok := model.ResolveTLSCerts(dir)
	if !ok {
		return resp
	}
	resp.Active = true
	resp.CertFile = certs.CertFile
	resp.KeyFile = certs.KeyFile
	resp.SelfSigned = isSelfSigned(certs.CertFile)
	if leaf := parseLeaf(certs.CertFile); leaf != nil {
		resp.NotAfter = leaf.NotAfter.Format(time.RFC3339)
		for _, ip := range leaf.IPAddresses {
			resp.Hosts = append(resp.Hosts, ip.String())
		}
		resp.Hosts = append(resp.Hosts, leaf.DNSNames...)
	}
	return resp
}

// httpsURLFor builds the TLS address of this server from the request.
//
// The port is reused verbatim: the server serves HTTP and HTTPS on the same
// listener (whichever the certificate enables), so switching does not move it.
func httpsURLFor(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return ""
	}
	return "https://" + net.JoinHostPort(host, portOf(r.Host, "20000"))
}

// portOf returns the port part of hostport, or fallback when there is none.
func portOf(hostport, fallback string) string {
	if _, p, err := net.SplitHostPort(hostport); err == nil && p != "" {
		return p
	}
	return fallback
}

// isSelfSigned reports whether the certificate is its own issuer.
func isSelfSigned(certFile string) bool {
	leaf := parseLeaf(certFile)
	if leaf == nil {
		return false
	}
	return leaf.Issuer.String() == leaf.Subject.String()
}

// parseLeaf reads the first certificate in a PEM file.
func parseLeaf(certFile string) *x509.Certificate {
	raw, err := os.ReadFile(filepath.Clean(certFile))
	if err != nil {
		return nil
	}
	for {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			return nil
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil
		}
		return cert
	}
}
