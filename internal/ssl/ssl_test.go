package ssl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"a.com_bundle.crt", "a.com"},
		{"a.com_bundle.key", "a.com"},
		{"b.com.crt", "b.com"},
		{"b.com.key", "b.com"},
		{"test.example.com_bundle.crt", "test.example.com"},
		{"invalid.txt", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := extractDomain(tt.filename)
		if result != tt.expected {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.filename, result, tt.expected)
		}
	}
}

func TestScanCertificates(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test certificate files
	certDir := filepath.Join(tmpDir, "certs")
	os.MkdirAll(certDir, 0755)

	// Create valid certificate pair
	os.WriteFile(filepath.Join(certDir, "a.com.crt"), []byte("cert"), 0644)
	os.WriteFile(filepath.Join(certDir, "a.com.key"), []byte("key"), 0644)

	// Create bundle certificate pair
	os.WriteFile(filepath.Join(certDir, "b.com_bundle.crt"), []byte("cert"), 0644)
	os.WriteFile(filepath.Join(certDir, "b.com_bundle.key"), []byte("key"), 0644)

	// Scan certificates
	certMap, err := ScanCertificates(tmpDir)
	if err != nil {
		t.Fatalf("Failed to scan certificates: %v", err)
	}

	if len(certMap) != 2 {
		t.Errorf("Expected 2 certificates, got %d", len(certMap))
	}

	if _, ok := certMap["a.com"]; !ok {
		t.Error("Certificate for a.com not found")
	}

	if _, ok := certMap["b.com"]; !ok {
		t.Error("Certificate for b.com not found")
	}
}

func TestScanCertificatesDuplicate(t *testing.T) {
	tmpDir := t.TempDir()

	// Create duplicate certificate files
	os.WriteFile(filepath.Join(tmpDir, "a.com.crt"), []byte("cert1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "a.com.key"), []byte("key1"), 0644)

	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "a.com.crt"), []byte("cert2"), 0644)
	os.WriteFile(filepath.Join(subDir, "a.com.key"), []byte("key2"), 0644)

	_, err := ScanCertificates(tmpDir)
	if err == nil {
		t.Error("Expected error for duplicate certificates")
	}
}
