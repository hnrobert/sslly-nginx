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
	matched, missing, expired := classifyDomains(cfg, activeCertMap, now)
	multiple := classifyMultipleCertificates(cfg, report)
	all := len(matched) + len(missing) + len(expired)

	logger.Info("Domain summary: total=%d matched=%d warning(no-cert)=%d warning(expired)=%d", all, len(matched), len(missing), len(expired))
	if all == 0 {
		return
	}

	if len(matched) > 0 {
		logger.Info("%s", formatDomainSection("Matched:", matched))
	}
	if len(missing) > 0 {
		logger.Warn("%s", formatDomainSection("No-cert:", missing))
	}
	if len(expired) > 0 {
		logger.Warn("%s", formatDomainSection("Expired:", expired))
	}
	if len(multiple) > 0 {
		logger.Warn("%s", formatMultipleCertSection("Multiple-certs:", multiple))
	}
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
			line += fmt.Sprintf(" (ignored other %d)", e.Ignored)
		}
		b.WriteString(line)
	}
	return b.String()
}

func classifyDomains(cfg *config.Config, activeCertMap map[string]ssl.Certificate, now time.Time) (matched, missing, expired []domainEntry) {
	baseDomains := collectBaseDomains(cfg)
	dests := collectDomainDestinations(cfg)

	// Build a map from baseDomain to all its domainPaths
	domainPaths := make(map[string][]string)
	if cfg != nil {
		for _, paths := range cfg.Ports {
			for _, domainPath := range paths {
				base := domainPath
				if idx := strings.Index(base, "/"); idx > 0 {
					base = base[:idx]
				}
				base = strings.ToLower(strings.TrimSpace(base))
				if base == "" {
					continue
				}
				key := strings.ToLower(strings.TrimSpace(domainPath))
				domainPaths[base] = append(domainPaths[base], key)
			}
		}
	}

	for domain := range baseDomains {
		cert, ok := activeCertMap[domain]
		paths := domainPaths[domain]
		if len(paths) == 0 {
			paths = []string{domain}
		}

		for _, domainPath := range paths {
			destinations := dests[domainPath]
			entry := domainEntry{Domain: domainPath, Destinations: destinations}
			if !ok {
				missing = append(missing, entry)
				continue
			}
			if !cert.NotAfter.IsZero() && !cert.NotAfter.After(now) {
				expired = append(expired, entry)
				continue
			}
			matched = append(matched, entry)
		}
	}

	sortDomainEntriesInPlace(matched)
	sortDomainEntriesInPlace(missing)
	sortDomainEntriesInPlace(expired)
	return matched, missing, expired
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
// For domain/path entries, domain is compared first, then path.
// Example ordering:
// abc.az
// abc.de
// abc.de/api
// abc.de/portainer
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

	// Split domain and path
	aDomain, aPath := a, ""
	if idx := strings.Index(a, "/"); idx > 0 {
		aDomain = a[:idx]
		aPath = a[idx:]
	}
	bDomain, bPath := b, ""
	if idx := strings.Index(b, "/"); idx > 0 {
		bDomain = b[:idx]
		bPath = b[idx:]
	}

	// Compare domains first
	if aDomain != bDomain {
		ap := strings.Split(aDomain, ".")
		bp := strings.Split(bDomain, ".")

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

	// Same domain, compare paths: no path < has path, then lexicographic
	if aPath == "" && bPath != "" {
		return true
	}
	if aPath != "" && bPath == "" {
		return false
	}
	return aPath < bPath
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
			// Use full domainPath as key to preserve path information
			key := strings.ToLower(strings.TrimSpace(domainPath))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = make(map[string]struct{})
			}
			seen[key][dest] = struct{}{}
		}
	}

	for domainPath, m := range seen {
		var list []string
		for d := range m {
			list = append(list, d)
		}
		sort.Strings(list)
		out[domainPath] = list
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
