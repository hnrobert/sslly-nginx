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

func logDomainSummary(cfg *config.Config, activeCertMap map[string]ssl.Certificate, now time.Time) {
	success, missing, expired := classifyDomains(cfg, activeCertMap, now)
	all := len(success) + len(missing) + len(expired)

	logger.Info("Domain summary: total=%d success=%d warning(no-cert)=%d warning(expired)=%d", all, len(success), len(missing), len(expired))
	if all == 0 {
		return
	}

	logger.Info(formatDomainBlock("success", success))
	if len(missing) > 0 {
		logger.Warn(formatDomainBlock("warning(no-cert)", missing))
	}
	if len(expired) > 0 {
		logger.Warn(formatDomainBlock("warning(expired)", expired))
	}
}

func formatDomainBlock(title string, entries []domainEntry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("%s: (none)", title)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s (%d):", title, len(entries)))
	for _, e := range entries {
		b.WriteString("\n  - ")
		b.WriteString(e.Domain)
		if len(e.Destinations) > 0 {
			b.WriteString(" -> ")
			b.WriteString(strings.Join(e.Destinations, ", "))
		}
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
