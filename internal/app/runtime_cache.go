package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

func collectBaseDomains(cfg *config.Config) map[string]struct{} {
	out := make(map[string]struct{})
	if cfg == nil {
		return out
	}
	for _, domainPaths := range cfg.Ports {
		for _, domainPath := range domainPaths {
			base := domainPath
			if idx := strings.Index(base, "/"); idx > 0 {
				base = base[:idx]
			}
			base = strings.ToLower(strings.TrimSpace(base))
			if base == "" {
				continue
			}
			out[base] = struct{}{}
		}
	}
	return out
}

func runtimeRootAbs() (string, error) {
	return filepath.Abs(runtimeDir)
}

func runtimeStageDirAbs(snapshotID string) (string, error) {
	root, err := runtimeRootAbs()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "stage", snapshotID), nil
}

func runtimeCurrentDirAbs() (string, error) {
	root, err := runtimeRootAbs()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "current"), nil
}

func runtimeOldDirAbs() (string, error) {
	root, err := runtimeRootAbs()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "old"), nil
}

func sanitizeDomainForFileName(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.ToLower(domain)
	if domain == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, domain)
}

func copyFileContents(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0777); err != nil {
		return err
	}
	if err := os.WriteFile(dstPath, data, 0666); err != nil {
		return err
	}
	return nil
}

func stageRuntimeCertificates(snapshotID string, cfg *config.Config, scanned map[string]ssl.Certificate) (map[string]ssl.Certificate, error) {
	stageDir, err := runtimeStageDirAbs(snapshotID)
	if err != nil {
		return nil, err
	}
	currentDir, err := runtimeCurrentDirAbs()
	if err != nil {
		return nil, err
	}

	// Fresh stage.
	_ = os.RemoveAll(stageDir)
	if err := os.MkdirAll(filepath.Join(stageDir, "certs"), 0777); err != nil {
		return nil, err
	}

	active := make(map[string]ssl.Certificate)
	for baseDomain := range collectBaseDomains(cfg) {
		cert, ok := ssl.FindCertificate(scanned, baseDomain)
		if !ok {
			continue
		}
		if cert.KeyPath == "" {
			continue
		}

		safe := sanitizeDomainForFileName(baseDomain)
		certExt := strings.ToLower(filepath.Ext(cert.CertPath))
		if certExt == "" {
			certExt = ".pem"
		}
		keyExt := strings.ToLower(filepath.Ext(cert.KeyPath))
		if keyExt == "" {
			keyExt = ".key"
		}

		stageCertName := safe + ".cert" + certExt
		stageKeyName := safe + ".key" + keyExt
		stageCertPath := filepath.Join(stageDir, "certs", stageCertName)
		stageKeyPath := filepath.Join(stageDir, "certs", stageKeyName)
		if err := copyFileContents(cert.CertPath, stageCertPath); err != nil {
			return nil, fmt.Errorf("copy cert for %s: %w", baseDomain, err)
		}
		if err := copyFileContents(cert.KeyPath, stageKeyPath); err != nil {
			return nil, fmt.Errorf("copy key for %s: %w", baseDomain, err)
		}

		active[baseDomain] = ssl.Certificate{
			CertPath: filepath.Join(currentDir, "certs", stageCertName),
			KeyPath:  filepath.Join(currentDir, "certs", stageKeyName),
			NotAfter: cert.NotAfter,
		}
	}

	return active, nil
}

func writeRuntimeNginxConf(snapshotID string, nginxConfig string) error {
	stageDir, err := runtimeStageDirAbs(snapshotID)
	if err != nil {
		return err
	}
	p := filepath.Join(stageDir, "nginx", "nginx.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0777); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(nginxConfig), 0666)
}

func activateRuntimeSnapshot(snapshotID string) error {
	stageDir, err := runtimeStageDirAbs(snapshotID)
	if err != nil {
		return err
	}
	currentDir, err := runtimeCurrentDirAbs()
	if err != nil {
		return err
	}
	oldDir, err := runtimeOldDirAbs()
	if err != nil {
		return err
	}
	root, err := runtimeRootAbs()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0777); err != nil {
		return err
	}

	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(currentDir); err == nil {
		if err := os.Rename(currentDir, oldDir); err != nil {
			return err
		}
	}
	// Ensure the stage exists.
	if _, err := os.Stat(stageDir); err != nil {
		// Best-effort rollback.
		_ = os.Rename(oldDir, currentDir)
		return err
	}
	if err := os.Rename(stageDir, currentDir); err != nil {
		// Best-effort rollback.
		_ = os.Rename(oldDir, currentDir)
		return err
	}
	return nil
}
