package ssl

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSelfSignedCertAndKey(t *testing.T, dir string, dnsNames []string) (certPath, keyPath string) {
	t.Helper()
	return writeSelfSignedCertAndKeyNamed(t, dir, "random-name.pem", "another-random.key", dnsNames)
}

func writeSelfSignedCertAndKeyNamed(t *testing.T, dir, certName, keyName string, dnsNames []string) (certPath, keyPath string) {
	t.Helper()
	return writeSelfSignedCertAndKeyNamedWithNotAfter(t, dir, certName, keyName, dnsNames, time.Now().Add(24*time.Hour))
}

func writeSelfSignedCertAndKeyNamedWithNotAfter(t *testing.T, dir, certName, keyName string, dnsNames []string, notAfter time.Time) (certPath, keyPath string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: dnsNames[0],
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames:              dnsNames,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	certPath = filepath.Join(dir, certName)
	keyPath = filepath.Join(dir, keyName)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certPath, keyPath
}

func TestScanCertificates(t *testing.T) {
	tmpDir := t.TempDir()

	certDir := filepath.Join(tmpDir, "certs")

	_, _ = writeSelfSignedCertAndKey(t, certDir, []string{"a.com"})
	_, _ = writeSelfSignedCertAndKey(t, filepath.Join(certDir, "sub"), []string{"b.com", "www.b.com"})

	// Scan certificates
	certMap, err := ScanCertificates(tmpDir)
	if err != nil {
		t.Fatalf("Failed to scan certificates: %v", err)
	}

	if _, ok := certMap["a.com"]; !ok {
		t.Error("Certificate for a.com not found")
	}
	if _, ok := certMap["b.com"]; !ok {
		t.Error("Certificate for b.com not found")
	}
	if _, ok := certMap["www.b.com"]; !ok {
		t.Error("Certificate for www.b.com not found")
	}

	if certMap["a.com"].KeyPath == "" {
		t.Error("Expected a.com to have matched private key")
	}
}

func TestScanCertificates_CrtExtension(t *testing.T) {
	tmpDir := t.TempDir()

	certDir := filepath.Join(tmpDir, "certs")
	// Same PEM content, but using .crt/.key filenames to ensure we don't depend on filename parsing.
	_, _ = writeSelfSignedCertAndKeyNamed(t, certDir, "server.crt", "server.key", []string{"crt.example.com"})

	certMap, err := ScanCertificates(tmpDir)
	if err != nil {
		t.Fatalf("Failed to scan certificates: %v", err)
	}
	if _, ok := certMap["crt.example.com"]; !ok {
		t.Fatal("Certificate for crt.example.com not found")
	}
	if certMap["crt.example.com"].KeyPath == "" {
		t.Fatal("Expected crt.example.com to have matched private key")
	}
}

func TestScanCertificatesDuplicate(t *testing.T) {
	tmpDir := t.TempDir()

	certDir1 := filepath.Join(tmpDir, "one")
	certDir2 := filepath.Join(tmpDir, "two")
	os.MkdirAll(certDir1, 0755)
	os.MkdirAll(certDir2, 0755)

	// Create two certs for the same domain; selection should prefer the one
	// with the farthest expiration time.
	soon := time.Now().Add(24 * time.Hour)
	later := time.Now().Add(48 * time.Hour)
	_, _ = writeSelfSignedCertAndKeyNamedWithNotAfter(t, certDir1, "a.crt", "a.key", []string{"a.com"}, soon)
	_, _ = writeSelfSignedCertAndKeyNamedWithNotAfter(t, certDir2, "a.pem", "a.key", []string{"a.com"}, later)

	certMap, report, err := ScanCertificatesWithReport(tmpDir)
	if err != nil {
		t.Fatalf("did not expect error for duplicate certificates: %v", err)
	}
	cert, ok := certMap["a.com"]
	if !ok {
		t.Fatal("expected a.com to exist in cert map")
	}
	if cert.NotAfter.Before(later.Add(-time.Minute)) {
		t.Fatalf("expected the later-expiring cert to be selected, got expiry: %s", cert.NotAfter)
	}
	if report.Multiple == nil {
		t.Fatalf("expected report.Multiple to be populated")
	}
	rep, ok := report.Multiple["a.com"]
	if !ok {
		t.Fatalf("expected a.com to be reported as multiple")
	}
	if rep.Selected.CertPath != cert.CertPath {
		t.Fatalf("report selected mismatch: got %s want %s", rep.Selected.CertPath, cert.CertPath)
	}
	if len(rep.All) < 2 {
		t.Fatalf("expected >=2 candidates, got %d", len(rep.All))
	}
}

func TestScanCertificatesDuplicateAllowedSamePriority(t *testing.T) {
	tmpDir := t.TempDir()

	certDir1 := filepath.Join(tmpDir, "one")
	certDir2 := filepath.Join(tmpDir, "two")
	os.MkdirAll(certDir1, 0755)
	os.MkdirAll(certDir2, 0755)

	_, _ = writeSelfSignedCertAndKeyNamed(t, certDir1, "x.pem", "x.key", []string{"dup.example.com"})
	_, _ = writeSelfSignedCertAndKeyNamed(t, certDir2, "y.pem", "y.key", []string{"dup.example.com"})

	_, report, err := ScanCertificatesWithReport(tmpDir)
	if err != nil {
		t.Fatalf("did not expect error for duplicate certificates: %v", err)
	}
	if report.Multiple == nil {
		t.Fatalf("expected report.Multiple to be populated")
	}
	if _, ok := report.Multiple["dup.example.com"]; !ok {
		t.Fatalf("expected dup.example.com to be reported as multiple")
	}
}

func TestFindCertificateWildcard(t *testing.T) {
	certMap := map[string]Certificate{
		"*.example.com": {CertPath: "/tmp/c.crt", KeyPath: "/tmp/c.key"},
	}
	if _, ok := FindCertificate(certMap, "a.example.com"); !ok {
		t.Fatal("expected wildcard match")
	}
	if _, ok := FindCertificate(certMap, "example.com"); ok {
		t.Fatal("did not expect wildcard to match apex domain")
	}
}
