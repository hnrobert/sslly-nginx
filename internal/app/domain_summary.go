package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

type domainEntry struct {
	Domain       string
	Destinations []string
}

type multipleCertEntry struct {
	Domain   string
	Selected string
	NotAfter time.Time
	Ignored  int
}

func logDomainSummary(cfg *config.Config, activeCertMap map[string]ssl.Certificate, report ssl.ScanReport, now time.Time) {
	success, missing, expired := classifyDomains(cfg, activeCertMap, now)
	multiple := classifyMultipleCertificates(cfg, report)
	all := len(success) + len(missing) + len(expired)

	logger.Info("Domain summary: total=%d matched=%d warning(no-cert)=%d warning(expired)=%d", all, len(success), len(missing), len(expired))
	if all == 0 {
		return
	}

	logger.Info("%s", formatDomainSection("Success:", success))
	logger.Warn("%s", formatDomainSection("No-cert:", missing))
	logger.Warn("%s", formatDomainSection("Expired:", expired))
	logger.Warn("%s", formatMultipleCertSection("Multi-certs:", multiple))
}

func formatDomainSection(header string, entries []domainEntry) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	if len(entries) == 0 {
		b.WriteString("  (none)")
		return b.String()
	}
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := "  - " + e.Domain
		if len(e.Destinations) > 0 {
			line += " -> " + strings.Join(e.Destinations, ", ")
		}
		b.WriteString(line)
	}
	return b.String()
}

func formatMultipleCertSection(header string, entries []multipleCertEntry) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	if len(entries) == 0 {
		b.WriteString("  (none)")
		return b.String()
	}
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := "  - " + e.Domain
		if e.Selected != "" {
			line += " -> " + e.Selected
			if !e.NotAfter.IsZero() {
				line += " (expires: " + e.NotAfter.UTC().Format(time.RFC3339) + ")"
			}
		}
		if e.Ignored > 0 {
			line += fmt.Sprintf(" (ignored: %d)", e.Ignored)
		}
		b.WriteString(line)
	}
	return b.String()
}

func classifyDomains(cfg *config.Config, activeCertMap map[string]ssl.Certificate, now time.Time) (success, missing, expired []domainEntry) {
	baseDomains := collectBaseDomains(cfg)
	dests := collectDomainDestinations(cfg)

	for domain := range baseDomains {
		cert, ok := activeCertMap[domain]
		entry := domainEntry{Domain: domain, Destinations: dests[domain]}
		if !ok {
			missing = append(missing, entry)
			continue
		}
		if !cert.NotAfter.IsZero() && !cert.NotAfter.After(now) {
			expired = append(expired, entry)
			continue
		}
		success = append(success, entry)
	}

	sortDomainEntriesInPlace(success)
	sortDomainEntriesInPlace(missing)
	sortDomainEntriesInPlace(expired)
	return success, missing, expired
}

func sortDomainEntriesInPlace(entries []domainEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return domainLess(entries[i].Domain, entries[j].Domain)
	})
}

func classifyMultipleCertificates(cfg *config.Config, report ssl.ScanReport) []multipleCertEntry {
	baseDomains := collectBaseDomains(cfg)
	if len(baseDomains) == 0 || len(report.Multiple) == 0 {
		return nil
	}

	var out []multipleCertEntry
	for domain, rep := range report.Multiple {
		d := strings.ToLower(strings.TrimSpace(domain))
		if _, ok := baseDomains[d]; !ok {
			continue
		}
		selected := rep.Selected.CertPath
		ignored := 0
		if n := len(rep.All); n > 1 {
			ignored = n - 1
		}
		out = append(out, multipleCertEntry{
			Domain:   d,
			Selected: selected,
			NotAfter: rep.Selected.NotAfter,
			Ignored:  ignored,
		})
	}

	sort.Slice(out, func(i, j int) bool { return domainLess(out[i].Domain, out[j].Domain) })
	return out
}

// domainLess sorts by labels from TLD -> left, comparing each label by Unicode codepoint.
// Example ordering:
// abc.az
// abc.de
// abc.abc.de
// aad.def
// abc.def
// abc.abc.def
func domainLess(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return false
	}

	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")

	ai := len(ap) - 1
	bi := len(bp) - 1
	for ai >= 0 && bi >= 0 {
		if ap[ai] != bp[bi] {
			return ap[ai] < bp[bi]
		}
		ai--
		bi--
	}
	// If all shared suffix labels match, shorter domain (fewer labels) sorts first.
	return len(ap) < len(bp)
}

func collectDomainDestinations(cfg *config.Config) map[string][]string {
	out := make(map[string][]string)
	if cfg == nil {
		return out
	}

	seen := make(map[string]map[string]struct{})
	for portKey, domainPaths := range cfg.Ports {
		up := config.ParseUpstream(portKey)
		dest := formatUpstreamDestination(up)
		for _, domainPath := range domainPaths {
			base := domainPath
			if idx := strings.Index(base, "/"); idx > 0 {
				base = base[:idx]
			}
			base = strings.ToLower(strings.TrimSpace(base))
			if base == "" {
				continue
			}
			if _, ok := seen[base]; !ok {
				seen[base] = make(map[string]struct{})
			}
			seen[base][dest] = struct{}{}
		}
	}

	for domain, m := range seen {
		var list []string
		for d := range m {
			list = append(list, d)
		}
		sort.Strings(list)
		out[domain] = list
	}
	return out
}

func formatUpstreamDestination(up config.Upstream) string {
	host := up.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	addr := fmt.Sprintf("%s:%s", host, up.Port)
	dest := fmt.Sprintf("%s://%s", up.Scheme, addr)
	if up.Path != "" {
		dest += up.Path
	}
	return dest
}
