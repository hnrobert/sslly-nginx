package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

	if err := os.MkdirAll(filepath.Dir(destPath), 0777); err != nil {
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

	// Do not chmod/chown bind-mounted paths at runtime.
	// Many environments deny permission changes, and we don't want to mutate host file permissions.

	logger.Info("Config file not found, copied default config: %s -> %s", defaultPath, destPath)
	return nil
}

// ensureDirWritable makes a directory and its existing contents writable by any user.
// For root-owned files/directories only, it sets permissions to 0777/0666.
// It does NOT change ownership.
func ensureDirWritable(dir string) error {
	// Create if not exists.
	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}

	// Fix permissions for root-owned files recursively (chmod only, no chown)
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // continue walking
		}

		// Check if owned by root (UID 0)
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return nil // skip non-root owned files
		}

		// Set permissions: directories to 0777, files to 0666
		if info.IsDir() {
			_ = os.Chmod(path, 0777)
		} else {
			_ = os.Chmod(path, 0666)
		}

		return nil
	})

	// Verify we can write to it.
	tmp, err := os.CreateTemp(dir, ".writable-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(name)

	return nil
}

func isInternalConfigPath(p string) bool {
	pp := filepath.ToSlash(p)
	return strings.Contains(pp, "/.sslly-backups/") || strings.Contains(pp, "/.sslly-runtime/")
}
