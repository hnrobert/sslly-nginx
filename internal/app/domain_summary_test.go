package app

import (
	"testing"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

func TestSortDomainsInPlace_TldFirstExample(t *testing.T) {
	domains := []string{
		"abc.abc.def",
		"abc.az",
		"abc.def",
		"aad.def",
		"abc.abc.de",
		"abc.de",
	}
	want := []string{
		"abc.az",
		"abc.de",
		"abc.abc.de",
		"aad.def",
		"abc.def",
		"abc.abc.def",
	}

	sortDomainsInPlace(domains)
	if len(domains) != len(want) {
		t.Fatalf("length mismatch: got %d want %d", len(domains), len(want))
	}
	for i := range want {
		if domains[i] != want[i] {
			t.Fatalf("order mismatch at %d: got %q want %q (full=%v)", i, domains[i], want[i], domains)
		}
	}
}

func TestClassifyDomains_SuccessMissingExpired(t *testing.T) {
	now := time.Date(2026, 1, 29, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{Ports: map[string][]string{
		"443": {
			"abc.de",
			"missing.de",
			"expired.de",
			"abc.az",
		},
	}}

	active := map[string]ssl.Certificate{
		"abc.de": {
			CertPath: "/runtime/current/certs/abc.de.pem",
			KeyPath:  "/runtime/current/certs/abc.de.key",
			NotAfter: now.Add(24 * time.Hour),
		},
		"expired.de": {
			CertPath: "/runtime/current/certs/expired.de.pem",
			KeyPath:  "/runtime/current/certs/expired.de.key",
			NotAfter: now.Add(-1 * time.Hour),
		},
		"abc.az": {
			CertPath: "/runtime/current/certs/abc.az.pem",
			KeyPath:  "/runtime/current/certs/abc.az.key",
			NotAfter: now.Add(365 * 24 * time.Hour),
		},
	}

	success, missing, expired := classifyDomains(cfg, active, now)
	if len(success) != 2 {
		t.Fatalf("success: got %v", success)
	}
	if len(missing) != 1 || missing[0] != "missing.de" {
		t.Fatalf("missing: got %v", missing)
	}
	if len(expired) != 1 || expired[0] != "expired.de" {
		t.Fatalf("expired: got %v", expired)
	}

	// Success list should be sorted by the domain comparator.
	if success[0] != "abc.az" || success[1] != "abc.de" {
		t.Fatalf("sorted success mismatch: got %v", success)
	}
}
