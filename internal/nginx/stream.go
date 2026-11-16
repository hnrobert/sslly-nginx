package nginx

import (
	"fmt"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

// StreamMapping represents a TCP/UDP stream mapping
type StreamMapping struct {
	ListenConfig config.ListenConfig
	Upstream     config.Upstream
}

// generateStreamBlock generates the nginx stream block for TCP/UDP forwarding
func generateStreamBlock(mappings []StreamMapping, httpsPort string) string {
	var sb strings.Builder
	sb.WriteString("stream {\n")

	// Check if we need ssl_preread (when TCP uses same port as HTTPS)
	needSSLPreread := false
	for _, m := range mappings {
		if m.ListenConfig.Protocol == config.ProtocolTCP && m.ListenConfig.Port == httpsPort {
			needSSLPreread = true
			break
		}
	}

	// Group mappings by listen port to detect conflicts
	portMappings := make(map[string][]StreamMapping)
	for _, m := range mappings {
		key := m.ListenConfig.Port
		if m.ListenConfig.Host != "" {
			key = m.ListenConfig.Host + ":" + m.ListenConfig.Port
		}
		portMappings[key] = append(portMappings[key], m)
	}

	// Generate upstreams and servers
	for _, m := range mappings {
		// Generate upstream
		upstreamName := fmt.Sprintf("stream_%s_%s", m.ListenConfig.Protocol, m.ListenConfig.Port)
		if m.ListenConfig.Host != "" {
			// Replace dots and colons for valid upstream name
			hostSafe := strings.ReplaceAll(m.ListenConfig.Host, ".", "_")
			hostSafe = strings.ReplaceAll(hostSafe, ":", "_")
			upstreamName = fmt.Sprintf("stream_%s_%s_%s", m.ListenConfig.Protocol, hostSafe, m.ListenConfig.Port)
		}

		upstreamAddr := formatUpstreamAddr(m.Upstream)

		fmt.Fprintf(&sb, "    upstream %s {\n", upstreamName)
		fmt.Fprintf(&sb, "        server %s;\n", upstreamAddr)
		sb.WriteString("    }\n\n")
	}

	// Handle ssl_preread for port 443 conflict
	if needSSLPreread {
		sb.WriteString("    # SSL preread for TLS/HTTPS detection on port 443\n")
		sb.WriteString("    map $ssl_preread_protocol $backend_443 {\n")
		sb.WriteString("        default stream_tcp_443;\n")
		sb.WriteString("        \"TLS\"  stream_https_443;\n")
		sb.WriteString("    }\n\n")

		// Generate the ssl_preread server
		sb.WriteString("    server {\n")
		fmt.Fprintf(&sb, "        listen %s;\n", httpsPort)
		sb.WriteString("        ssl_preread on;\n")
		sb.WriteString("        proxy_pass $backend_443;\n")
		sb.WriteString("    }\n\n")
	}

	// Generate servers for each mapping
	for _, m := range mappings {
		// Skip the httpsPort if we're using ssl_preread
		if needSSLPreread && m.ListenConfig.Protocol == config.ProtocolTCP && m.ListenConfig.Port == httpsPort {
			continue
		}

		upstreamName := fmt.Sprintf("stream_%s_%s", m.ListenConfig.Protocol, m.ListenConfig.Port)
		if m.ListenConfig.Host != "" {
			hostSafe := strings.ReplaceAll(m.ListenConfig.Host, ".", "_")
			hostSafe = strings.ReplaceAll(hostSafe, ":", "_")
			upstreamName = fmt.Sprintf("stream_%s_%s_%s", m.ListenConfig.Protocol, hostSafe, m.ListenConfig.Port)
		}

		sb.WriteString("    server {\n")

		// Build listen directive
		listenAddr := m.ListenConfig.Port
		if m.ListenConfig.Host != "" {
			listenAddr = fmt.Sprintf("%s:%s", m.ListenConfig.Host, m.ListenConfig.Port)
		}

		if m.ListenConfig.Protocol == config.ProtocolUDP {
			fmt.Fprintf(&sb, "        listen %s udp;\n", listenAddr)
		} else {
			fmt.Fprintf(&sb, "        listen %s;\n", listenAddr)
		}

		fmt.Fprintf(&sb, "        proxy_pass %s;\n", upstreamName)
		sb.WriteString("    }\n\n")
	}

	sb.WriteString("}\n\n")
	return sb.String()
}
