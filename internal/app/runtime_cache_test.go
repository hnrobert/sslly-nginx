package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

func TestStageRuntimeCertificates_DistinctNamesForPemCertAndKey(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	certPath := filepath.Join(tmp, "wws.pub.pem")
	keyPath := filepath.Join(tmp, "wws.key.pem")

	certBody := "-----BEGIN CERTIFICATE-----\nCERT-DATA\n-----END CERTIFICATE-----\n"
	keyBody := "-----BEGIN PRIVATE KEY-----\nKEY-DATA\n-----END PRIVATE KEY-----\n"
	if err := os.WriteFile(certPath, []byte(certBody), 0644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte(keyBody), 0644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cfg := &config.Config{
		Ports: map[string][]string{
			"8000": {"example.com"},
		},
	}
	scanned := map[string]ssl.Certificate{
		"example.com": {
			CertPath: certPath,
			KeyPath:  keyPath,
			NotAfter: time.Now().Add(24 * time.Hour),
		},
	}

	active, err := stageRuntimeCertificates("snap1", cfg, scanned)
	if err != nil {
		t.Fatalf("stageRuntimeCertificates error: %v", err)
	}
	got, ok := active["example.com"]
	if !ok {
		t.Fatalf("missing active certificate for example.com")
	}
	if got.CertPath == got.KeyPath {
		t.Fatalf("cert and key path must differ, got same: %s", got.CertPath)
	}
	if !strings.Contains(got.CertPath, ".cert.pem") {
		t.Fatalf("expected cert path to include .cert.pem, got %s", got.CertPath)
	}
	if !strings.Contains(got.KeyPath, ".key.pem") {
		t.Fatalf("expected key path to include .key.pem, got %s", got.KeyPath)
	}

	stageCertPath := filepath.Join(tmp, "configs", ".sslly-runtime", "stage", "snap1", "certs", "example.com.cert.pem")
	stageKeyPath := filepath.Join(tmp, "configs", ".sslly-runtime", "stage", "snap1", "certs", "example.com.key.pem")

	certBytes, err := os.ReadFile(stageCertPath)
	if err != nil {
		t.Fatalf("read staged cert: %v", err)
	}
	keyBytes, err := os.ReadFile(stageKeyPath)
	if err != nil {
		t.Fatalf("read staged key: %v", err)
	}
	if string(certBytes) != certBody {
		t.Fatalf("staged cert content mismatch")
	}
	if string(keyBytes) != keyBody {
		t.Fatalf("staged key content mismatch")
	}
}
