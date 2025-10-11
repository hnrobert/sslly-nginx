package ssl

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Certificate struct {
	CertPath string
	KeyPath  string
}

// ScanCertificates recursively scans the SSL directory for certificates
func ScanCertificates(sslDir string) (map[string]Certificate, error) {
	certMap := make(map[string]Certificate)
	duplicates := make(map[string][]string)

	err := filepath.Walk(sslDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Check for certificate files
		filename := info.Name()
		domain := extractDomain(filename)
		if domain == "" {
			return nil
		}

		// Check if this is a cert or key file
		if strings.HasSuffix(filename, ".crt") {
			keyPath := strings.TrimSuffix(path, ".crt") + ".key"
			if _, err := os.Stat(keyPath); err == nil {
				if existing, exists := certMap[domain]; exists {
					duplicates[domain] = append(duplicates[domain], existing.CertPath, path)
				} else {
					certMap[domain] = Certificate{
						CertPath: path,
						KeyPath:  keyPath,
					}
					log.Printf("Found certificate for domain: %s (cert: %s, key: %s)", domain, path, keyPath)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan SSL directory: %w", err)
	}

	// Check for duplicates
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
