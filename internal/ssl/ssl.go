package ssl

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/logger"
)

type Certificate struct {
	CertPath string
	KeyPath  string
	NotAfter time.Time
}

// ScanCertificates recursively scans the SSL directory for certificates
func ScanCertificates(sslDir string) (map[string]Certificate, error) {
	// Convert sslDir to absolute path first
	absSslDir, err := filepath.Abs(sslDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for SSL directory: %w", err)
	}

	certMap := make(map[string]Certificate)

	err = filepath.Walk(absSslDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Detect certificate files by *content*, not by filename.
		domains, leaf, err := readCertificateDomains(path)
		if err != nil {
			// Not a cert (or unreadable) -> ignore.
			return nil
		}
		if leaf == nil || len(domains) == 0 {
			return nil
		}

		keyPath := ""
		if p, ok := findMatchingPrivateKeyInDir(filepath.Dir(path), leaf); ok {
			keyPath = p
		}
		// Requirement: must have a matching cert+key pair to be considered valid for TLS.
		if keyPath == "" {
			return nil
		}

		candidate := Certificate{CertPath: path, KeyPath: keyPath, NotAfter: leaf.NotAfter}

		for _, domain := range domains {
			if prev, exists := certMap[domain]; exists {
				if isBetterCertificate(prev, candidate) {
					certMap[domain] = candidate
					logger.Warn("Multiple certificates for domain %s, prefer %s (expires %s) over %s (expires %s)", domain, path, leaf.NotAfter.UTC().Format(time.RFC3339), prev.CertPath, prev.NotAfter.UTC().Format(time.RFC3339))
				} else {
					logger.Warn("Multiple certificates for domain %s, keep %s (expires %s), ignore %s (expires %s)", domain, prev.CertPath, prev.NotAfter.UTC().Format(time.RFC3339), path, leaf.NotAfter.UTC().Format(time.RFC3339))
				}
				continue
			}
			certMap[domain] = candidate
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan SSL directory: %w", err)
	}

	logger.Info("SSL scan completed: %d domains have valid certificate+key pairs", len(certMap))

	return certMap, nil
}

func isBetterCertificate(existing Certificate, candidate Certificate) bool {
	// Primary rule: choose the certificate that expires latest.
	if candidate.NotAfter.After(existing.NotAfter) {
		return true
	}
	if existing.NotAfter.After(candidate.NotAfter) {
		return false
	}

	// Secondary rule: prefer pem > crt when expirations are equal.
	existingPriority := certificatePathPriority(existing.CertPath)
	candidatePriority := certificatePathPriority(candidate.CertPath)
	if candidatePriority != existingPriority {
		return candidatePriority > existingPriority
	}

	// Deterministic tie-breakers.
	if !strings.EqualFold(candidate.CertPath, existing.CertPath) {
		return strings.ToLower(candidate.CertPath) < strings.ToLower(existing.CertPath)
	}
	return strings.ToLower(candidate.KeyPath) < strings.ToLower(existing.KeyPath)
}

// certificatePathPriority defines precedence among duplicate certificates when other factors are equal.
func certificatePathPriority(path string) int {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pem":
		return 2
	case ".crt":
		return 1
	default:
		return 0
	}
}

// FindCertificate tries exact match first, then wildcard matches (e.g. "*.example.com").
func FindCertificate(certMap map[string]Certificate, domain string) (Certificate, bool) {
	if certMap == nil {
		return Certificate{}, false
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if cert, ok := certMap[domain]; ok {
		return cert, true
	}
	for pat, cert := range certMap {
		if strings.HasPrefix(pat, "*.") {
			suffix := pat[1:] // ".example.com"
			if strings.HasSuffix(domain, suffix) && domain != pat[2:] {
				return cert, true
			}
		}
	}
	return Certificate{}, false
}

func readCertificateDomains(path string) ([]string, *x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	certs, err := parseCertificates(data)
	if err != nil || len(certs) == 0 {
		return nil, nil, fmt.Errorf("not a certificate")
	}

	leaf := pickLeafCertificate(certs)
	if leaf == nil {
		return nil, nil, fmt.Errorf("no leaf certificate")
	}

	domains := extractDomainsFromCert(leaf)
	if len(domains) == 0 {
		return nil, leaf, fmt.Errorf("no dns names")
	}
	return domains, leaf, nil
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	// PEM path
	var certs []*x509.Certificate
	rest := data
	for {
		block, r := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = r
		if block.Type != "CERTIFICATE" && block.Type != "TRUSTED CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certs = append(certs, c)
	}
	if len(certs) > 0 {
		return certs, nil
	}

	// DER path
	if c, err := x509.ParseCertificate(data); err == nil {
		return []*x509.Certificate{c}, nil
	}
	return nil, fmt.Errorf("no certificate data")
}

func pickLeafCertificate(certs []*x509.Certificate) *x509.Certificate {
	for _, c := range certs {
		if c != nil && !c.IsCA {
			return c
		}
	}
	if len(certs) > 0 {
		return certs[0]
	}
	return nil
}

func extractDomainsFromCert(cert *x509.Certificate) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, d := range cert.DNSNames {
		add(d)
	}
	if cert.Subject.CommonName != "" {
		add(cert.Subject.CommonName)
	}
	return out
}

func findMatchingPrivateKeyInDir(dir string, cert *x509.Certificate) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	certPub, err := publicKeyBytes(cert.PublicKey)
	if err != nil {
		return "", false
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		key, err := parsePrivateKey(data)
		if err != nil {
			continue
		}
		pub := publicKeyFromPrivate(key)
		if pub == nil {
			continue
		}
		keyPub, err := publicKeyBytes(pub)
		if err != nil {
			continue
		}
		if bytes.Equal(certPub, keyPub) {
			return p, true
		}
	}

	return "", false
}

func parsePrivateKey(data []byte) (crypto.PrivateKey, error) {
	// Try PEM first (may contain multiple blocks)
	rest := data
	for {
		block, r := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = r
		if strings.Contains(block.Type, "PRIVATE KEY") {
			if block.Type == "RSA PRIVATE KEY" {
				if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
					return k, nil
				}
				continue
			}
			if block.Type == "EC PRIVATE KEY" {
				if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
					return k, nil
				}
				continue
			}
			if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
				return k, nil
			}
		}
	}

	// DER fallbacks
	if k, err := x509.ParsePKCS1PrivateKey(data); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(data); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(data); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("not a private key")
}

func publicKeyFromPrivate(k crypto.PrivateKey) crypto.PublicKey {
	switch key := k.(type) {
	case *rsa.PrivateKey:
		return key.Public()
	case *ecdsa.PrivateKey:
		return key.Public()
	case ed25519.PrivateKey:
		return key.Public()
	default:
		return nil
	}
}

func publicKeyBytes(pub crypto.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}
