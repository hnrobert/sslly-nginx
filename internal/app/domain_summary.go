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

func formatDomainBlock(title string, domains []string) string {
	if len(domains) == 0 {
		return fmt.Sprintf("%s: (none)", title)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s (%d):", title, len(domains)))
	for _, d := range domains {
		b.WriteString("\n  - ")
		b.WriteString(d)
	}
	return b.String()
}

func classifyDomains(cfg *config.Config, activeCertMap map[string]ssl.Certificate, now time.Time) (success, missing, expired []string) {
	baseDomains := collectBaseDomains(cfg)
	for domain := range baseDomains {
		cert, ok := activeCertMap[domain]
		if !ok {
			missing = append(missing, domain)
			continue
		}
		if !cert.NotAfter.IsZero() && !cert.NotAfter.After(now) {
			expired = append(expired, domain)
			continue
		}
		success = append(success, domain)
	}

	sortDomainsInPlace(success)
	sortDomainsInPlace(missing)
	sortDomainsInPlace(expired)
	return success, missing, expired
}

func sortDomainsInPlace(domains []string) {
	sort.Slice(domains, func(i, j int) bool {
		return domainLess(domains[i], domains[j])
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
