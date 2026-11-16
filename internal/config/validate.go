package config

import (
	"fmt"
	"strings"
)

type MappingError struct {
	Key     string // The upstream_key that has the error
	Value   string // The listener_key that has the error (if applicable)
	Message string // Error description
}

func (e *MappingError) Error() string {
	if e.Value != "" {
		return fmt.Sprintf("mapping error for %q -> %q: %s", e.Key, e.Value, e.Message)
	}
	return fmt.Sprintf("mapping error for %q: %s", e.Key, e.Message)
}

type MappingWarning struct {
	Key     string // The upstream_key that has the warning
	Value   string // The listener_key that has the warning (if applicable)
	Message string // Warning description
}

func (w *MappingWarning) String() string {
	if w.Value != "" {
		return fmt.Sprintf("mapping warning for %q -> %q: %s", w.Key, w.Value, w.Message)
	}
	return fmt.Sprintf("mapping warning for %q: %s", w.Key, w.Message)
}

func ValidateMapping(upstreamKey string, listenerKey string, hasCertificate bool) (ListenConfig, []error, []*MappingWarning) {
	var errors []error
	var warnings []*MappingWarning

	// Parse upstream
	upstream := ParseUpstream(upstreamKey)

	// Parse listener (may have explicit protocol or not)
	listenConfig := ParseListenKey(listenerKey)
	explicitProtocol := strings.Contains(listenerKey, "<") && strings.Contains(listenerKey, ">")

	// Rule 1: listen_protocol cannot be static
	if listenConfig.Protocol == ProtocolStatic {
		errors = append(errors, &MappingError{
			Key:     upstreamKey,
			Value:   listenerKey,
			Message: "listen_protocol cannot be 'static'; static sites are only for upstream configuration",
		})
		return listenConfig, errors, warnings
	}

	// gRPC upstreams: no path-based routing on either side — gRPC method
	// paths (/package.Service/Method) cannot live under a location prefix,
	// and the upstream address has no URL path component either.
	if upstream.Protocol.IsGRPC() {
		if upstream.Path != "" {
			errors = append(errors, &MappingError{
				Key:     upstreamKey,
				Value:   listenerKey,
				Message: "gRPC upstreams do not support a path suffix; remove it from the upstream key",
			})
			return listenConfig, errors, warnings
		}
		if strings.Contains(listenerKey, "/") {
			errors = append(errors, &MappingError{
				Key:     upstreamKey,
				Value:   listenerKey,
				Message: "gRPC upstreams do not support path-based routing; the listener must be a bare domain (optionally with |port)",
			})
			return listenConfig, errors, warnings
		}
	}

	// Determine effective listen protocol
	if upstream.Protocol.IsStream() {
		// Upstream is TCP or UDP
		if explicitProtocol {
			// User explicitly specified listen protocol
			if listenConfig.Protocol != upstream.Protocol {
				// Different protocol - this is an error
				errors = append(errors, &MappingError{
					Key:     upstreamKey,
					Value:   listenerKey,
					Message: fmt.Sprintf("listen_protocol '%s' does not match upstream protocol '%s'; this mapping will be ignored", listenConfig.Protocol, upstream.Protocol),
				})
				return listenConfig, errors, warnings
			}
			// Same protocol - redundant but allowed (warning)
			warnings = append(warnings, &MappingWarning{
				Key:     upstreamKey,
				Value:   listenerKey,
				Message: fmt.Sprintf("explicit listen_protocol '%s' is redundant when upstream is also '%s'; you can omit the <protocol> prefix", listenConfig.Protocol, upstream.Protocol),
			})
		} else {
			// Smart mode: use upstream protocol
			listenConfig.Protocol = upstream.Protocol
		}
	} else if upstream.Protocol.IsHTTP() || upstream.Protocol.IsGRPC() || upstream.Protocol == ProtocolStatic {
		// Upstream is HTTP, HTTPS, or Static
		if explicitProtocol {
			// User explicitly specified listen protocol
			if !listenConfig.Protocol.IsHTTP() {
				// Non-HTTP listen for HTTP/HTTPS/Static upstream - error
				errors = append(errors, &MappingError{
					Key:     upstreamKey,
					Value:   listenerKey,
					Message: fmt.Sprintf("listen_protocol '%s' is not compatible with upstream protocol '%s'; only http/https are allowed", listenConfig.Protocol, upstream.Protocol),
				})
				return listenConfig, errors, warnings
			}
			// Explicit http/https is allowed
		} else {
			// Smart mode: determine based on certificate
			// If has certificate or upstream is https -> https
			// Otherwise -> http
			if hasCertificate || upstream.Protocol == ProtocolHTTPS {
				listenConfig.Protocol = ProtocolHTTPS
			} else {
				listenConfig.Protocol = ProtocolHTTP
			}
		}
	}

	return listenConfig, errors, warnings
}

type ValidatedMapping struct {
	UpstreamKey  string
	ListenerKey  string
	Upstream     Upstream
	ListenConfig ListenConfig
	Errors       []error
	Warnings     []*MappingWarning
}

func ValidateConfig(cfg *Config, certMap map[string]bool) ([]ValidatedMapping, []error, []*MappingWarning) {
	var validMappings []ValidatedMapping
	var allErrors []error
	var allWarnings []*MappingWarning

	for upstreamKey, listenerKeys := range cfg.Ports {
		// Skip static site keys - they are handled separately
		if IsStaticSiteKey(upstreamKey) {
			continue
		}

		// For TCP/UDP upstreams, the listenerKeys are actually upstream targets
		// We need to check if this is a stream mapping
		upstream := ParseUpstream(upstreamKey)
		if upstream.Protocol.IsStream() {
			// Stream mapping: upstreamKey is the listen side, listenerKeys are targets
			for _, targetKey := range listenerKeys {
				listenConfig, errors, warnings := ValidateMapping(upstreamKey, targetKey, false)
				mapping := ValidatedMapping{
					UpstreamKey:  upstreamKey,
					ListenerKey:  targetKey,
					Upstream:     upstream,
					ListenConfig: listenConfig,
					Errors:       errors,
					Warnings:     warnings,
				}
				allErrors = append(allErrors, errors...)
				allWarnings = append(allWarnings, warnings...)

				if len(errors) == 0 {
					validMappings = append(validMappings, mapping)
				}
			}
		} else {
			// HTTP/HTTPS/Static mapping: upstreamKey is the target, listenerKeys are domains
			for _, listenerKey := range listenerKeys {
				// Extract domain from listenerKey to check certificate
				domain := listenerKey
				if idx := strings.Index(listenerKey, "/"); idx > 0 {
					domain = listenerKey[:idx]
				}
				hasCert := certMap[domain]

				listenConfig, errors, warnings := ValidateMapping(upstreamKey, listenerKey, hasCert)
				mapping := ValidatedMapping{
					UpstreamKey:  upstreamKey,
					ListenerKey:  listenerKey,
					Upstream:     upstream,
					ListenConfig: listenConfig,
					Errors:       errors,
					Warnings:     warnings,
				}
				allErrors = append(allErrors, errors...)
				allWarnings = append(allWarnings, warnings...)

				if len(errors) == 0 {
					validMappings = append(validMappings, mapping)
				}
			}
		}
	}

	return validMappings, allErrors, allWarnings
}
