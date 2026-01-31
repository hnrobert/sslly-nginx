package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/logger"
)

func ensureConfigFile(destPath, defaultPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if _, err := os.Stat(defaultPath); err != nil {
		return fmt.Errorf("default config not found at %s: %w", defaultPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	src, err := os.Open(defaultPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	// Ensure permissive permissions so host user can write if necessary
	// Files: 0666 (rw for all), Dirs: 0777 (rwx for all)
	if err := os.Chmod(destPath, 0666); err != nil {
		logger.Warn("failed to chmod %s: %v", destPath, err)
	}
	if err := os.Chmod(filepath.Dir(destPath), 0777); err != nil {
		logger.Warn("failed to chmod %s: %v", filepath.Dir(destPath), err)
	}

	// If running as root inside the image, attempt to chown files to UID/GID 1000:1000
	if os.Geteuid() == 0 {
		if err := os.Chown(destPath, 1000, 1000); err != nil {
			logger.Warn("failed to chown %s: %v", destPath, err)
		}
		if err := os.Chown(filepath.Dir(destPath), 1000, 1000); err != nil {
			logger.Warn("failed to chown %s: %v", filepath.Dir(destPath), err)
		}
	}

	logger.Info("Config file not found, copied default config: %s -> %s", defaultPath, destPath)
	return nil
}

// ensureDirWritable makes a directory and its existing contents writable by any user,
// and attempts to chown to UID/GID 1000 when running as root.
func ensureDirWritable(dir string) error {
	// Create if not exists
	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}

	// Walk entries and set permissive permissions
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := os.Chmod(p, 0777); err != nil {
				logger.Warn("failed to chmod dir %s: %v", p, err)
			}
		} else {
			if err := os.Chmod(p, 0666); err != nil {
				logger.Warn("failed to chmod file %s: %v", p, err)
			}
		}
		// Attempt chown if root
		if os.Geteuid() == 0 {
			if err := os.Chown(p, 1000, 1000); err != nil {
				// Not fatal
				logger.Warn("failed to chown %s: %v", p, err)
			}
		}
	}

	// Finally ensure dir itself has permissive perms and ownership
	if err := os.Chmod(dir, 0777); err != nil {
		logger.Warn("failed to chmod dir %s: %v", dir, err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(dir, 1000, 1000); err != nil {
			logger.Warn("failed to chown dir %s: %v", dir, err)
		}
	}

	return nil
}

func isInternalConfigPath(p string) bool {
	pp := filepath.ToSlash(p)
	return strings.Contains(pp, "/.sslly-backups/") || strings.Contains(pp, "/.sslly-runtime/")
}
