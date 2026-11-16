package config

import "strings"

func ParseStaticSiteKey(key string) (StaticSiteSpec, bool, error) {
	k := strings.TrimSpace(strings.TrimSuffix(key, ":"))
	if k == "" {
		return StaticSiteSpec{}, false, nil
	}

	// Check and ignore <protocol> prefix (should not be used for static sites)
	if strings.HasPrefix(k, "<") {
		closeIdx := strings.Index(k, ">")
		if closeIdx > 0 {
			k = strings.TrimSpace(k[closeIdx+1:])
		}
	}

	// Only absolute paths starting with '/' are static sites
	if !strings.HasPrefix(k, "/") {
		return StaticSiteSpec{}, false, nil
	}

	// Check for '//' separator (directory//route)
	if doubleSlashIdx := strings.Index(k, "//"); doubleSlashIdx > 0 {
		dir := k[:doubleSlashIdx]
		routePath := k[doubleSlashIdx+2:]

		if routePath == "" {
			return StaticSiteSpec{Dir: dir, RoutePath: ""}, true, nil
		}
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		return StaticSiteSpec{Dir: dir, RoutePath: normalizeRoutePath(routePath)}, true, nil
	}

	return StaticSiteSpec{Dir: k}, true, nil
}

func normalizeRoutePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "/" {
		return "/"
	}
	for strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "" {
		return "/"
	}
	return p
}

func ParseUpstream(key string) Upstream {
	// Remove trailing colon if present (for YAML keys like "192.168.31.6:1234:")
	key = strings.TrimSuffix(key, ":")

	// Check for protocol prefix
	// Format: <https>, <http>, <tcp>, <udp>
	protocol := ProtocolHTTP
	if strings.HasPrefix(key, "<") {
		closeIdx := strings.Index(key, ">")
		if closeIdx > 0 {
			protoStr := strings.ToLower(key[1:closeIdx])
			protocol = Protocol(protoStr)
			key = strings.TrimSpace(key[closeIdx+1:])
		}
	}

	scheme := "http"
	if protocol == ProtocolHTTPS {
		scheme = "https"
	}

	// Check for path suffix (e.g., "/api")
	path := ""
	if slashIdx := strings.Index(key, "/"); slashIdx > 0 {
		path = key[slashIdx:]
		key = key[:slashIdx]
	}

	// Handle IPv6 format [host]:port
	if strings.HasPrefix(key, "[") {
		closeBracket := strings.Index(key, "]")
		if closeBracket > 0 && closeBracket < len(key)-1 && key[closeBracket+1] == ':' {
			return Upstream{
				Scheme:   scheme,
				Protocol: protocol,
				Host:     key[1:closeBracket],
				Port:     key[closeBracket+2:],
				Path:     path,
			}
		}
	}

	// Check if key contains a colon (IP:port or hostname:port format)
	if strings.Contains(key, ":") {
		// Use LastIndex to handle cases like "::1:9000" (split from the last colon)
		lastColon := strings.LastIndex(key, ":")
		host := key[:lastColon]
		port := key[lastColon+1:]

		// If host part is empty or port part contains colon, it's likely plain port or invalid
		// Examples: ":8080" should be treated as port 8080, "::1:9000" needs special handling
		if host == "" || strings.Contains(port, ":") {
			// If it looks like IPv6 (multiple colons and no brackets), treat whole thing as plain port
			if strings.Count(key, ":") > 1 {
				// This is likely malformed - default to treating as plain port
				return Upstream{
					Scheme:   scheme,
					Protocol: protocol,
					Host:     "127.0.0.1",
					Port:     key,
					Path:     path,
				}
			}
			// Single colon at start (:8080) - treat as plain port
			if host == "" {
				return Upstream{
					Scheme:   scheme,
					Protocol: protocol,
					Host:     "127.0.0.1",
					Port:     port,
					Path:     path,
				}
			}
		}

		// Valid host:port format (could be IP or hostname)
		return Upstream{
			Scheme:   scheme,
			Protocol: protocol,
			Host:     host,
			Port:     port,
			Path:     path,
		}
	}

	// Check if it's a pure number (port only)
	if isNumeric(key) {
		return Upstream{
			Scheme:   scheme,
			Protocol: protocol,
			Host:     "127.0.0.1",
			Port:     key,
			Path:     path,
		}
	}

	// Otherwise it's a hostname/domain without port - use default port
	defaultPort := "80"
	if scheme == "https" {
		defaultPort = "443"
	}
	if protocol == ProtocolGRPC {
		defaultPort = "50051" // conventional gRPC port
	}
	return Upstream{
		Scheme:   scheme,
		Protocol: protocol,
		Host:     key,
		Port:     defaultPort,
		Path:     path,
	}
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func ParseListenKey(key string) ListenConfig {
	// Remove trailing colon if present (for YAML keys)
	key = strings.TrimSuffix(key, ":")

	// Default protocol is HTTP
	protocol := ProtocolHTTP

	// Check for protocol prefix <protocol>
	if strings.HasPrefix(key, "<") {
		closeIdx := strings.Index(key, ">")
		if closeIdx > 0 {
			protoStr := strings.ToLower(key[1:closeIdx])
			protocol = Protocol(protoStr)
			key = strings.TrimSpace(key[closeIdx+1:])
		}
	}

	// Check for server_name|port format
	if strings.Contains(key, "|") {
		pipeIdx := strings.Index(key, "|")
		host := key[:pipeIdx]
		port := key[pipeIdx+1:]

		// Handle IPv6 format [host]|port
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = host[1 : len(host)-1]
		}

		return ListenConfig{
			Protocol: protocol,
			Host:     host,
			Port:     port,
		}
	}

	// Just a port number (no | separator)
	return ListenConfig{
		Protocol: protocol,
		Host:     "",
		Port:     key,
	}
}

func IsStaticSiteKey(key string) bool {
	k := strings.TrimSpace(key)

	// Check and ignore <protocol> prefix
	if strings.HasPrefix(k, "<") {
		closeIdx := strings.Index(k, ">")
		if closeIdx > 0 {
			k = strings.TrimSpace(k[closeIdx+1:])
		}
	}

	// Bracket syntax: [DIR]/route - only if DIR starts with '.' or '/'
	if strings.HasPrefix(k, "[") {
		closeIdx := strings.Index(k, "]")
		if closeIdx > 0 {
			inside := strings.TrimSpace(k[1:closeIdx])
			if strings.HasPrefix(inside, ".") || strings.HasPrefix(inside, "/") {
				return true
			}
		}
		return false
	}

	// Simple path syntax
	return strings.HasPrefix(k, ".") || strings.HasPrefix(k, "/")
}
