package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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

// Protocol represents the protocol type for listen/upstream configuration
type Protocol string

const (
	ProtocolHTTP   Protocol = "http"
	ProtocolHTTPS  Protocol = "https"
	ProtocolTCP    Protocol = "tcp"
	ProtocolUDP    Protocol = "udp"
	ProtocolStatic Protocol = "static"
)

func exampleDir() string {
	if v := os.Getenv("SSLLY_EXAMPLE_DIR"); v != "" {
		return v
	}
	return exampleDirDefault
}

// CORSConfig represents CORS configuration for a domain or wildcard
type CORSConfig struct {
	AllowOrigin      string   `yaml:"allow_origin"`      // Access-Control-Allow-Origin (default: "*")
	AllowMethods     []string `yaml:"allow_methods"`     // Access-Control-Allow-Methods
	AllowHeaders     []string `yaml:"allow_headers"`     // Access-Control-Allow-Headers
	ExposeHeaders    []string `yaml:"expose_headers"`    // Access-Control-Expose-Headers
	MaxAge           int      `yaml:"max_age"`           // Access-Control-Max-Age in seconds (default: 1728000)
	AllowCredentials bool     `yaml:"allow_credentials"` // Access-Control-Allow-Credentials (default: false)
}

// LogLevelConfig represents log level configuration for a component
type LogLevelConfig struct {
	Level string `yaml:"level"` // Log level: debug, info, warn, error (case insensitive, default: info)
}

// NginxLogConfig represents nginx-specific log configuration
type NginxLogConfig struct {
	Level      string `yaml:"level"`       // Display log level: debug, info, warn, error (default: info)
	StderrAs   string `yaml:"stderr_as"`   // Nginx stderr log level: warn or error (default: error)
	StderrShow string `yaml:"stderr_show"` // Display stderr as: warn or error (default: same as stderr_as)
}

// LogConfig represents logging configuration
type LogConfig struct {
	SSLLY LogLevelConfig `yaml:"sslly"` // SSLLY-NGINX component log level
	Nginx NginxLogConfig `yaml:"nginx"` // NGINX-PROCS component log configuration
}

// Upstream represents a backend server configuration
type Upstream struct {
	Scheme   string   // Protocol scheme: "http" or "https" (default: "http") - legacy field, use Protocol for new code
	Protocol Protocol // Protocol type: http, https, tcp, udp, static
	Host     string   // IP address or hostname (default: 127.0.0.1)
	Port     string   // Port number
	Path     string   // Optional path prefix for routing (HTTP/HTTPS only)
}

// ListenConfig represents a listening configuration
type ListenConfig struct {
	Protocol Protocol // Listen protocol: http, https, tcp, udp
	Host     string   // Listen address (empty = all interfaces)
	Port     string   // Listen port
}

// IsHTTP returns true if the protocol is HTTP or HTTPS
func (p Protocol) IsHTTP() bool {
	return p == ProtocolHTTP || p == ProtocolHTTPS
}

// IsStream returns true if the protocol is TCP or UDP (stream layer)
func (p Protocol) IsStream() bool {
	return p == ProtocolTCP || p == ProtocolUDP
}

// IsStatic returns true if the protocol is static (file serving)
func (p Protocol) IsStatic() bool {
	return p == ProtocolStatic
}

type Config struct {
	Log   LogConfig             `yaml:"log"`
	CORS  map[string]CORSConfig `yaml:"cors"`
	Ports map[string][]string   `yaml:",inline"`

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

// ParseStaticSiteKey parses a proxy.yaml mapping key that represents a local static directory.
//
// Rules:
//   - Only keys starting with '/' are treated as static sites.
//   - Use '//' to separate directory from route path: /app/static//docs
//   - Colons ':' are treated as part of the file path (NOT as port separators).
//   - If the key has a <protocol> prefix, it is ignored.
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

// ParseUpstream parses the key format which can be:
// - "1234" -> Upstream{Scheme: "http", Host: "127.0.0.1", Port: "1234", Path: ""}
// - "192.168.31.6:1234" -> Upstream{Scheme: "http", Host: "192.168.31.6", Port: "1234", Path: ""}
// - "192.168.31.6:1234/api" -> Upstream{Scheme: "http", Host: "192.168.31.6", Port: "1234", Path: "/api"}
// - "[::1]:9000" -> Upstream{Scheme: "http", Host: "::1", Port: "9000", Path: ""} (IPv6 format)
// - "example-server.local:8080" -> Upstream{Scheme: "http", Host: "example-server.local", Port: "8080", Path: ""}
// - "<https>192.168.50.2:1234" -> Upstream{Scheme: "https", Host: "192.168.50.2", Port: "1234", Path: ""}
// - "<https>www.baidu.com" -> Upstream{Scheme: "https", Host: "www.baidu.com", Port: "443", Path: ""}
// - "www.example.com" -> Upstream{Scheme: "http", Host: "www.example.com", Port: "80", Path: ""}
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

// ParseListenKey parses a listen key which can be:
// - "1234" -> ListenConfig{Protocol: http, Host: "", Port: "1234"} (HTTP, listen on all interfaces)
// - "server_name|1234" -> ListenConfig{Protocol: http, Host: "server_name", Port: "1234"}
// - "<http>1234" -> ListenConfig{Protocol: http, Host: "", Port: "1234"} (explicit HTTP)
// - "<https>443" -> ListenConfig{Protocol: https, Host: "", Port: "443"} (HTTPS)
// - "<tcp>9122" -> ListenConfig{Protocol: tcp, Host: "", Port: "9122"} (TCP)
// - "<tcp>192.168.50.1|22" -> ListenConfig{Protocol: tcp, Host: "192.168.50.1", Port: "22"} (TCP on specific IP)
// - "<udp>9123" -> ListenConfig{Protocol: udp, Host: "", Port: "9123"} (UDP)
// The | separator is used to split server_name and port (colon is used for IPv6 addresses)
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

// IsStaticSiteKey returns true if the key appears to be a static site mapping
// (starts with '.' or '/', or uses the [dir]/route bracket syntax with a static dir)
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

func Load(configDir string) (*Config, error) {
	if err := Prepare(configDir); err != nil {
		return nil, err
	}

	proxyPath := filepath.Join(configDir, proxyConfigFile)
	proxyData, err := os.ReadFile(proxyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", proxyConfigFile, err)
	}

	var config Config
	if err := yaml.Unmarshal(proxyData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", proxyConfigFile, err)
	}

	// Load optional logs config (content is the inner object, without outer 'log:')
	logsPath := filepath.Join(configDir, logsConfigFile)
	if data, err := os.ReadFile(logsPath); err == nil {
		var logsCfg LogConfig
		if err := yaml.Unmarshal(data, &logsCfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", logsConfigFile, err)
		}
		config.Log = logsCfg
	}

	// Load optional CORS config (content is the inner object, without outer 'cors:')
	corsPath := filepath.Join(configDir, corsConfigFile)
	if data, err := os.ReadFile(corsPath); err == nil {
		var corsCfg map[string]CORSConfig
		if err := yaml.Unmarshal(data, &corsCfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", corsConfigFile, err)
		}
		config.CORS = corsCfg
	}

	// Defensive: do not allow these keys to appear as ports.
	delete(config.Ports, "cors")
	delete(config.Ports, "log")

	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("config is empty or invalid (%s has no proxy mappings)", proxyConfigFile)
	}

	return &config, nil
}

// Prepare ensures the configuration directory is ready for loading:
// - If legacy config.yaml/config.yml exists, it is migrated to split files.
// - If split files are missing, they are created from the corresponding example files.
func Prepare(configDir string) error {
	if err := migrateLegacyConfigIfPresent(configDir); err != nil {
		return err
	}
	return ensureSplitConfigFiles(configDir)
}

func migrateLegacyConfigIfPresent(configDir string) error {
	legacyPaths := []string{
		filepath.Join(configDir, legacyConfigYAML),
		filepath.Join(configDir, legacyConfigYML),
	}

	var legacyPath string
	for _, p := range legacyPaths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			legacyPath = p
			break
		}
	}
	if legacyPath == "" {
		return nil
	}

	proxyPath := filepath.Join(configDir, proxyConfigFile)
	corsPath := filepath.Join(configDir, corsConfigFile)
	logsPath := filepath.Join(configDir, logsConfigFile)

	anySplitExists := fileExists(proxyPath) || fileExists(corsPath) || fileExists(logsPath)
	if !anySplitExists {
		data, err := os.ReadFile(legacyPath)
		if err != nil {
			return fmt.Errorf("failed to read legacy config %s: %w", filepath.Base(legacyPath), err)
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("failed to parse legacy config %s: %w", filepath.Base(legacyPath), err)
		}

		proxyDoc, corsDoc, logsDoc, err := splitLegacyDocPreserveComments(&doc)
		if err != nil {
			return err
		}

		// Extract cors/log blocks (if any). These files contain the inner object, without outer 'cors:'/'log:'.
		if corsDoc != nil && !fileExists(corsPath) {
			if err := writeYAMLNodeFile(corsPath, corsDoc); err != nil {
				return err
			}
		}
		if logsDoc != nil && !fileExists(logsPath) {
			if err := writeYAMLNodeFile(logsPath, logsDoc); err != nil {
				return err
			}
		}
		if !fileExists(proxyPath) {
			if err := writeYAMLNodeFile(proxyPath, proxyDoc); err != nil {
				return err
			}
		}
	}

	// After splitting, ensure any missing files are created from examples.
	if err := ensureSplitConfigFiles(configDir); err != nil {
		return err
	}

	// Finally, rename legacy config to config.backup.yaml (or .yml)
	legacyBase := filepath.Base(legacyPath)
	backupName := "config.backup" + filepath.Ext(legacyBase)
	backupPath := filepath.Join(configDir, backupName)
	if fileExists(backupPath) {
		ts := time.Now().UTC().Format("20060102T150405Z")
		backupName = "config.backup." + ts + filepath.Ext(legacyBase)
		backupPath = filepath.Join(configDir, backupName)
	}
	if err := os.Rename(legacyPath, backupPath); err != nil {
		return fmt.Errorf("failed to rename legacy config to %s: %w", backupName, err)
	}
	return nil
}

func splitLegacyDocPreserveComments(doc *yaml.Node) (proxyDoc, corsDoc, logsDoc *yaml.Node, err error) {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, nil, fmt.Errorf("invalid legacy config: expected a YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, nil, fmt.Errorf("invalid legacy config: expected a mapping at the document root")
	}

	proxyDoc = deepCopyYAMLNode(doc)
	proxyRoot := proxyDoc.Content[0]

	var corsKey, corsVal, logKey, logVal *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "cors":
			corsKey = deepCopyYAMLNode(k)
			corsVal = deepCopyYAMLNode(v)
		case "log":
			logKey = deepCopyYAMLNode(k)
			logVal = deepCopyYAMLNode(v)
		}
	}

	// Remove extracted keys from proxy output.
	filtered := proxyRoot.Content[:0]
	for i := 0; i+1 < len(proxyRoot.Content); i += 2 {
		k := proxyRoot.Content[i]
		if k.Kind == yaml.ScalarNode && (k.Value == "cors" || k.Value == "log") {
			continue
		}
		filtered = append(filtered, k, proxyRoot.Content[i+1])
	}
	proxyRoot.Content = filtered

	if corsVal != nil {
		mergeDroppedKeyComments(corsKey, corsVal)
		corsDoc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{corsVal}}
	}
	if logVal != nil {
		mergeDroppedKeyComments(logKey, logVal)
		logsDoc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{logVal}}
	}

	return proxyDoc, corsDoc, logsDoc, nil
}

func deepCopyYAMLNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cpy := *n
	if n.Alias != nil {
		cpy.Alias = deepCopyYAMLNode(n.Alias)
	}
	if len(n.Content) > 0 {
		cpy.Content = make([]*yaml.Node, 0, len(n.Content))
		for _, c := range n.Content {
			cpy.Content = append(cpy.Content, deepCopyYAMLNode(c))
		}
	}
	return &cpy
}

func mergeDroppedKeyComments(key, val *yaml.Node) {
	if key == nil || val == nil {
		return
	}

	// Comments that were attached to the removed key should still be preserved.
	if key.HeadComment != "" {
		val.HeadComment = joinComments(key.HeadComment, val.HeadComment)
	}
	if key.LineComment != "" {
		// Inline comment on the dropped key can't remain inline; preserve it as a head comment.
		val.HeadComment = joinComments(key.LineComment, val.HeadComment)
	}
	if key.FootComment != "" {
		val.FootComment = joinComments(val.FootComment, key.FootComment)
	}
}

func joinComments(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n" + b
}

func writeYAMLNodeFile(path string, doc *yaml.Node) error {
	if doc == nil {
		return fmt.Errorf("failed to write %s: nil yaml document", filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	defer file.Close()

	enc := yaml.NewEncoder(file)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("failed to encode yaml for %s: %w", filepath.Base(path), err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to finalize yaml for %s: %w", filepath.Base(path), err)
	}
	return nil
}

func ensureSplitConfigFiles(configDir string) error {
	if err := ensureFileFromExample(configDir, proxyConfigFile, proxyExampleFile); err != nil {
		return err
	}
	if err := ensureFileFromExample(configDir, corsConfigFile, corsExampleFile); err != nil {
		return err
	}
	if err := ensureFileFromExample(configDir, logsConfigFile, logsExampleFile); err != nil {
		return err
	}
	return nil
}

func ensureFileFromExample(configDir, filename, exampleFilename string) error {
	dst := filepath.Join(configDir, filename)
	if fileExists(dst) {
		return nil
	}
	// Prefer split example file
	examplePath := filepath.Join(exampleDir(), exampleFilename)
	if fileExists(examplePath) {
		return copyFile(examplePath, dst)
	}

	// proxy.yaml is required; do not silently create an empty config.
	if filename == proxyConfigFile {
		return fmt.Errorf("missing required %s and no example found at %s", proxyConfigFile, examplePath)
	}

	// Optional files: keep behaviour of ensuring they exist.
	return os.WriteFile(dst, []byte{}, 0666)
}

func writeYAMLFile(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml for %s: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, data, 0666); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filepath.Base(src), err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0777); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(dst), err)
	}
	if err := os.WriteFile(dst, data, 0666); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(dst), err)
	}
	return nil
}

// MappingError represents a validation error for a proxy mapping
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

// MappingWarning represents a validation warning for a proxy mapping
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

// ValidateMapping validates a single proxy mapping (upstream_key -> listener_key)
// Returns the effective listen config, any errors, and any warnings.
//
// Validation rules:
// 1. listen_protocol cannot be static
// 2. If upstream is http/https, listen can only be http or https (or auto-detect)
// 3. If upstream is tcp/udp, listen should match (warning if same, error if different)
// 4. Smart mode: if listen_protocol is not specified, it's inferred from upstream
//   - tcp upstream -> tcp listen
//   - http/https upstream -> http/https listen based on certificate availability (handled by caller)
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
	} else if upstream.Protocol.IsHTTP() || upstream.Protocol == ProtocolStatic {
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

// ValidatedMapping represents a validated proxy mapping
type ValidatedMapping struct {
	UpstreamKey  string
	ListenerKey  string
	Upstream     Upstream
	ListenConfig ListenConfig
	Errors       []error
	Warnings     []*MappingWarning
}

// ValidateConfig validates all proxy mappings in the config.
// Returns validated mappings (including those with errors for logging),
// and a boolean indicating if there were any fatal errors.
//
// Mappings with errors are excluded from the returned slice,
// but their errors are collected for reporting.
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
