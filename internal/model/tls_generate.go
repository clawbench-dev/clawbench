package model

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SelfSignedCertYears is how long a generated certificate stays valid. Long on
// purpose: an expired certificate silently breaks every client on the LAN, and
// there is no renewal machinery for a self-signed one.
const SelfSignedCertYears = 10

// LocalTLSSANs returns the names a self-signed certificate must cover for this
// machine to be reachable by the addresses the app advertises.
//
// Loopback and "localhost" are always included. Every non-loopback IPv4 address
// is added because that is what colleagues type into the share link; a missing
// address produces ERR_CERT_COMMON_NAME_INVALID, which looks like "the
// certificate is broken" rather than "this address was not listed".
//
// IPv6 is deliberately omitted: the app's address list prefers IPv4 and a
// certificate is only checked against the address actually used, so adding
// every temporary RFC 4941 address would just bloat the SAN without helping.
func LocalTLSSANs() (ips []net.IP, dnsNames []string) {
	ips = append(ips, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))
	dnsNames = append(dnsNames, "localhost")

	ifaces, err := net.Interfaces()
	if err != nil {
		return ips, dnsNames
	}
	seen := map[string]bool{"127.0.0.1": true}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			ip := ipNet.IP.To4()
			if seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			ips = append(ips, ip)
		}
	}
	return ips, dnsNames
}

// GenerateSelfSignedCert writes cert.pem and key.pem into dir, covering every
// host in ips/dnsNames.
//
// The SAN list is what makes this usable: browsers ignore Common Name entirely
// and require the address to appear in subjectAltName, so a certificate for an
// IP without `IP:` entries is rejected with a name-mismatch error even after
// the user accepts the self-signed warning.
//
// The key is ECDSA P-256 — universally supported, fast to generate, and small
// enough that the PEM fits comfortably in any client's trust prompt.
func GenerateSelfSignedCert(dir string, ips []net.IP, dnsNames []string) (TLSCerts, error) {
	if dir == "" {
		return TLSCerts{}, errors.New("tls: cert directory is not configured")
	}
	if len(ips) == 0 && len(dnsNames) == 0 {
		return TLSCerts{}, errors.New("tls: certificate would cover no address")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return TLSCerts{}, fmt.Errorf("tls: create cert dir: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return TLSCerts{}, fmt.Errorf("tls: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return TLSCerts{}, fmt.Errorf("tls: generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			// Cosmetic only: browsers read the SAN, never this.
			CommonName: "ClawBench",
		},
		// Backdated an hour so a client whose clock is slightly behind does not
		// reject a certificate that is not valid yet.
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(SelfSignedCertYears, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return TLSCerts{}, fmt.Errorf("tls: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return TLSCerts{}, fmt.Errorf("tls: marshal key: %w", err)
	}

	certPath := filepath.Join(dir, tlsCertFile)
	keyPath := filepath.Join(dir, tlsKeyFile)
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return TLSCerts{}, fmt.Errorf("tls: write cert: %w", err)
	}
	// 0600: the private key is the only secret here. WriteFile only applies the
	// mode when it CREATES the file, so a key left behind by an older run (or by
	// another tool) keeps its wider permissions unless we chmod explicitly.
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return TLSCerts{}, fmt.Errorf("tls: write key: %w", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return TLSCerts{}, fmt.Errorf("tls: restrict key: %w", err)
	}
	return TLSCerts{CertFile: certPath, KeyFile: keyPath}, nil
}

// DescribeTLSSANs renders the covered hosts for the CLI's confirmation output.
func DescribeTLSSANs(ips []net.IP, dnsNames []string) string {
	parts := make([]string, 0, len(dnsNames)+len(ips))
	parts = append(parts, dnsNames...)
	for _, ip := range ips {
		parts = append(parts, ip.String())
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
