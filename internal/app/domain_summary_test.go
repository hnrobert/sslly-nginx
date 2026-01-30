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
		"abc.de/api",
		"abc.de/portainer",
	}
	want := []string{
		"abc.az",
		"abc.de",
		"abc.de/api",
		"abc.de/portainer",
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

	matched, missing, expired := classifyDomains(cfg, active, now)
	if len(matched) != 3 {
		t.Fatalf("matched: got %v", matched)
	}
	if len(missing) != 1 || missing[0].Domain != "missing.de" {
		t.Fatalf("missing: got %v", missing)
	}
	if len(expired) != 1 || expired[0].Domain != "expired.de" {
		t.Fatalf("expired: got %v", expired)
	}

	// Success list should be sorted by the domain comparator.
	// Now we expect abc.az, abc.de, abc.de/api (separate entries for different paths)
	if matched[0].Domain != "abc.az" || matched[1].Domain != "abc.de" || matched[2].Domain != "abc.de/api" {
		t.Fatalf("sorted success mismatch: got %v", matched)
	}

	// Ensure destinations are included and include scheme/host/port/path.
	if len(matched[1].Destinations) == 0 {
		t.Fatalf("expected destinations for %s", matched[1].Domain)
	}
	// matched[1] is abc.de with destination http://127.0.0.1:1234
	// matched[2] is abc.de/api with destination https://[::1]:9000/api
	foundHTTP := false
	for _, d := range matched[1].Destinations {
		if d == "http://127.0.0.1:1234" {
			foundHTTP = true
		}
	}
	if !foundHTTP {
		t.Fatalf("expected http destination in %v", matched[1].Destinations)
	}

	if len(matched[2].Destinations) == 0 {
		t.Fatalf("expected destinations for %s", matched[2].Domain)
	}
	foundIPv6HTTPS := false
	for _, d := range matched[2].Destinations {
		if d == "https://[::1]:9000/api" {
			foundIPv6HTTPS = true
		}
	}
	if !foundIPv6HTTPS {
		t.Fatalf("expected ipv6 https destination in %v", matched[2].Destinations)
	}
}

func TestClassifyMultipleCertificates_ConfigDomainsOnlyAndSorted(t *testing.T) {
	cfg := &config.Config{Ports: map[string][]string{
		"1234": {"abc.de", "abc.az", "unused.example"},
	}}

	report := ssl.ScanReport{Multiple: map[string]*ssl.MultipleCertificateReport{
		"abc.de":                {Selected: ssl.Certificate{CertPath: "/ssl/abc.de.pem", NotAfter: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)}, All: []ssl.Certificate{{CertPath: "/ssl/abc.de.pem"}, {CertPath: "/ssl/abc.de.crt"}}},
		"abc.az":                {Selected: ssl.Certificate{CertPath: "/ssl/abc.az.pem"}, All: []ssl.Certificate{{CertPath: "/ssl/abc.az.pem"}, {CertPath: "/ssl/abc.az.crt"}, {CertPath: "/ssl/abc.az.old.pem"}}},
		"not-in-config.example": {Selected: ssl.Certificate{CertPath: "/ssl/nope.pem"}, All: []ssl.Certificate{{CertPath: "/ssl/nope.pem"}, {CertPath: "/ssl/nope2.pem"}}},
	}}

	entries := classifyMultipleCertificates(cfg, report)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %v", entries)
	}
	if entries[0].Domain != "abc.az" || entries[1].Domain != "abc.de" {
		t.Fatalf("expected sorted domains abc.az then abc.de, got %v", entries)
	}
	if entries[0].Ignored != 2 {
		t.Fatalf("expected abc.az ignored=2, got %d", entries[0].Ignored)
	}
	if entries[1].Ignored != 1 {
		t.Fatalf("expected abc.de ignored=1, got %d", entries[1].Ignored)
	}
}
