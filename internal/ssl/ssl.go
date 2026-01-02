package ssl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/logger"
)

type Certificate struct {
	CertPath string
	KeyPath  string
}

// ScanCertificates recursively scans the SSL directory for certificates
func ScanCertificates(sslDir string) (map[string]Certificate, error) {
	// Convert sslDir to absolute path first
	absSslDir, err := filepath.Abs(sslDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for SSL directory: %w", err)
	}

	// We'll first collect candidates for certs and keys by a normalized domain name
	// Normalization: strip trailing _bundle from base name so domain_bundle.crt and domain.key map to same domain
	type candidate struct {
		certs []string
		keys  []string
	}

	candidates := make(map[string]*candidate)
	duplicates := make(map[string][]string)

	err = filepath.Walk(absSslDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		filename := info.Name()
		// determine if file looks like cert or key
		lower := strings.ToLower(filename)
		var domain string
		if strings.HasSuffix(lower, ".crt") || strings.HasSuffix(lower, ".key") {
			domain = extractDomain(filename)
		}
		if domain == "" {
			return nil
		}

		// normalize domain by removing trailing _bundle if present
		norm := strings.TrimSuffix(domain, "_bundle")

		c := candidates[norm]
		if c == nil {
			c = &candidate{}
			candidates[norm] = c
		}

		if strings.HasSuffix(lower, ".crt") {
			c.certs = append(c.certs, path)
		} else if strings.HasSuffix(lower, ".key") {
			c.keys = append(c.keys, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan SSL directory: %w", err)
	}

	certMap := make(map[string]Certificate)

	// Pair up certs and keys for each normalized domain
	for norm, c := range candidates {
		if len(c.certs) == 0 || len(c.keys) == 0 {
			// no complete pair
			continue
		}

		// If multiple certs or keys exist for same normalized domain, treat as duplicate
		if len(c.certs) > 1 || len(c.keys) > 1 {
			duplicates[norm] = append(duplicates[norm], append(c.certs, c.keys...)...)
			continue
		}

		certPath := c.certs[0]
		keyPath := c.keys[0]

		// determine final domain key to store: prefer plain domain over _bundle variant
		// extractDomain on filenames returns original base (may include _bundle); prefer without
		finalDomain := norm

		// store
		certMap[finalDomain] = Certificate{CertPath: certPath, KeyPath: keyPath}
		logger.Info("Found certificate for domain: %s (cert: %s, key: %s)", finalDomain, certPath, keyPath)
	}

	if len(duplicates) > 0 {
		for domain, paths := range duplicates {
			return nil, fmt.Errorf("duplicate certificates found for domain %s: %v", domain, paths)
		}
	}

	return certMap, nil
}

// extractDomain extracts the domain name from certificate filename
func extractDomain(filename string) string {
	// Remove extension
	name := filename

	// Pattern: domain_bundle.crt/key
	if strings.HasSuffix(name, "_bundle.crt") {
		return strings.TrimSuffix(name, "_bundle.crt")
	}
	if strings.HasSuffix(name, "_bundle.key") {
		return strings.TrimSuffix(name, "_bundle.key")
	}

	// Pattern: domain.crt/key
	if strings.HasSuffix(name, ".crt") {
		return strings.TrimSuffix(name, ".crt")
	}
	if strings.HasSuffix(name, ".key") {
		return strings.TrimSuffix(name, ".key")
	}

	return ""
}
