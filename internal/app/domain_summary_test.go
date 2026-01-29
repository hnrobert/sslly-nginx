package app

import (
	"sort"
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

	// Sort uses the same domain comparator used for log entries.
	sort.Slice(domains, func(i, j int) bool { return domainLess(domains[i], domains[j]) })
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
		"1234": {
			"abc.de",
			"missing.de",
			"expired.de",
			"abc.az",
		},
		"[https][::1]:9000/api": {"abc.de/api"},
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
	if len(missing) != 1 || missing[0].Domain != "missing.de" {
		t.Fatalf("missing: got %v", missing)
	}
	if len(expired) != 1 || expired[0].Domain != "expired.de" {
		t.Fatalf("expired: got %v", expired)
	}

	// Success list should be sorted by the domain comparator.
	if success[0].Domain != "abc.az" || success[1].Domain != "abc.de" {
		t.Fatalf("sorted success mismatch: got %v", success)
	}

	// Ensure destinations are included and include scheme/host/port/path.
	if len(success[1].Destinations) == 0 {
		t.Fatalf("expected destinations for %s", success[1].Domain)
	}
	// From 1234 default config.ParseUpstream should be http://127.0.0.1:1234
	foundHTTP := false
	foundIPv6HTTPS := false
	for _, d := range success[1].Destinations {
		if d == "http://127.0.0.1:1234" {
			foundHTTP = true
		}
		if d == "https://[::1]:9000/api" {
			foundIPv6HTTPS = true
		}
	}
	if !foundHTTP {
		t.Fatalf("expected http destination in %v", success[1].Destinations)
	}
	if !foundIPv6HTTPS {
		t.Fatalf("expected ipv6 https destination in %v", success[1].Destinations)
	}
}
