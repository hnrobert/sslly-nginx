package config

import "os"

const (
	legacyConfigYAML = "config.yaml"
	legacyConfigYML  = "config.yml"

	proxyConfigFile = "proxy.yaml"
	corsConfigFile  = "cors.yaml"
	logsConfigFile  = "logs.yaml"

	exampleDirDefault = "/etc/sslly/configs/"

	proxyExampleFile = "proxy.example.yaml"
	corsExampleFile  = "cors.example.yaml"
	logsExampleFile  = "logs.example.yaml"
)

// Exported live config file names, used by the control API layer.
const (
	ProxyConfigFile = proxyConfigFile
	CorsConfigFile  = corsConfigFile
	LogsConfigFile  = logsConfigFile
	UsersConfigFile = usersConfigFile
)

const (
	ProtocolHTTP   Protocol = "http"
	ProtocolHTTPS  Protocol = "https"
	ProtocolTCP    Protocol = "tcp"
	ProtocolUDP    Protocol = "udp"
	ProtocolStatic Protocol = "static"
	ProtocolGRPC   Protocol = "grpc"
)

type Protocol string

func exampleDir() string {
	if v := os.Getenv("SSLLY_EXAMPLE_DIR"); v != "" {
		return v
	}
	return exampleDirDefault
}

type LogLevelConfig struct {
	Level string `yaml:"level"` // Log level: debug, info, warn, error (case insensitive, default: info)
}

type NginxLogConfig struct {
	Level      string `yaml:"level"`       // Display log level: debug, info, warn, error (default: info)
	StderrAs   string `yaml:"stderr_as"`   // Nginx stderr log level: warn or error (default: error)
	StderrShow string `yaml:"stderr_show"` // Display stderr as: warn or error (default: same as stderr_as)
}

type LogConfig struct {
	SSLLY LogLevelConfig `yaml:"sslly"` // SSLLY-NGINX component log level
	Nginx NginxLogConfig `yaml:"nginx"` // NGINX-PROCS component log configuration
}

type Upstream struct {
	Scheme   string   // Protocol scheme: "http" or "https" (default: "http") - legacy field, use Protocol for new code
	Protocol Protocol // Protocol type: http, https, tcp, udp, static
	Host     string   // IP address or hostname (default: 127.0.0.1)
	Port     string   // Port number
	Path     string   // Optional path prefix for routing (HTTP/HTTPS only)
}

type ListenConfig struct {
	Protocol Protocol // Listen protocol: http, https, tcp, udp
	Host     string   // Listen address (empty = all interfaces)
	Port     string   // Listen port
}

func (p Protocol) IsHTTP() bool {
	return p == ProtocolHTTP || p == ProtocolHTTPS
}

func (p Protocol) IsStream() bool {
	return p == ProtocolTCP || p == ProtocolUDP
}

func (p Protocol) IsStatic() bool {
	return p == ProtocolStatic
}

func (p Protocol) IsGRPC() bool {
	return p == ProtocolGRPC
}

type Config struct {
	Log             LogConfig             `yaml:"log"`
	CORS            map[string]CORSConfig `yaml:"cors"`
	NoTrailingSlash []string              `yaml:"no_trailing_slash"`
	Ports           map[string][]string   `yaml:",inline"`

	// OrderedPorts holds the top-level proxy.yaml mapping keys in their file
	// order (excluding the special cors/log/no_trailing_slash keys). It is
	// used to emit nginx server blocks deterministically, in declaration order.
	// Runtime-only (not persisted to YAML).
	OrderedPorts []string `yaml:"-"`

	// RuntimeStaticSites stores static site information for nginx config generation.
	// Key is the original config key (e.g., "/app/static" or "[/app/static]/route").
	// It is runtime-only (not persisted to YAML).
	RuntimeStaticSites map[string]StaticSiteSpec `yaml:"-"`
}

type StaticSiteSpec struct {
	Dir string
	// RoutePath is an optional URL path prefix (e.g. "/home") used to build domain/path routes.
	// It is NOT a filesystem path.
	RoutePath string
}
