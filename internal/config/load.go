package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

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
	if err := parseProxyDoc(proxyData, &config); err != nil {
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

	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("config is empty or invalid (%s has no proxy mappings)", proxyConfigFile)
	}

	return &config, nil
}

// reservedGroupNames may not be used as group path segments: they are (or
// collide with) the special top-level keys of proxy.yaml.
var reservedGroupNames = map[string]bool{
	"cors": true, "log": true, "no_trailing_slash": true,
}

// validGroupNameSeg reports whether s is a legal single group-path segment
// ([a-zA-Z0-9_-]+, no dots — dots only separate segments).
func validGroupNameSeg(s string) bool {
	if s == "" || reservedGroupNames[s] {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// SplitGroupPath validates and splits a dotted group path ("a.b") into its
// segments. Empty input returns nil (top level).
func SplitGroupPath(group string) ([]string, error) {
	if group == "" {
		return nil, nil
	}
	segs := strings.Split(group, ".")
	for _, s := range segs {
		if !validGroupNameSeg(s) {
			return nil, fmt.Errorf("invalid group %q: each dot-separated segment must match [a-zA-Z0-9_-]+ and not be a reserved key", group)
		}
	}
	return segs, nil
}

// parseProxyDoc parses proxy.yaml from its yaml.Node tree so nested GROUP
// mappings are understood alongside plain route entries:
//
//	1234:                 # sequence value  -> plain route (top level)
//	  - a.com
//	class1:               # mapping value   -> group (nested form)
//	  1234:
//	    - asd.asd.com
//	a.b:                  # mapping value   -> group (flat dotted form)
//	  8080:
//	    - c.com
//
// A mapping value always denotes a group; a sequence value always denotes a
// route — that holds at any depth, so upstream keys containing dots (e.g.
// "example-server.local:8080") stay unambiguous as long as their value is a
// listener sequence. Groups are purely organizational: after flattening,
// Ports/OrderedPorts have the same semantics as before, and EntryGroups
// records which groups each upstream key appeared in (declaration order).
func parseProxyDoc(data []byte, config *Config) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	config.Ports = make(map[string][]string)
	config.EntryGroups = make(map[string][]string)

	seenOrder := make(map[string]bool)
	addEntry := func(group string, upstreamKey string, m *yaml.Node) error {
		var listeners []string
		if err := m.Decode(&listeners); err != nil {
			return fmt.Errorf("listeners of %q: %w", upstreamKey, err)
		}
		if _, dup := config.Ports[upstreamKey]; !dup {
			config.Ports[upstreamKey] = listeners
			seenOrder[upstreamKey] = true
			config.OrderedPorts = append(config.OrderedPorts, upstreamKey)
		} else {
			// The same upstream key in several groups (or group + top level):
			// merge listeners in declaration order.
			config.Ports[upstreamKey] = append(config.Ports[upstreamKey], listeners...)
		}
		if group != "" {
			config.EntryGroups[upstreamKey] = append(config.EntryGroups[upstreamKey], group)
		}
		return nil
	}

	var walk func(path []string, m *yaml.Node) error
	walk = func(path []string, m *yaml.Node) error {
		group := strings.Join(path, ".")
		for i := 0; i+1 < len(m.Content); i += 2 {
			k, v := m.Content[i], m.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				return fmt.Errorf("non-scalar key inside group %q", group)
			}
			switch v.Kind {
			case yaml.SequenceNode:
				if err := addEntry(group, k.Value, v); err != nil {
					return err
				}
			case yaml.MappingNode:
				// A nested mapping is a subgroup. A dotted key splits into
				// several levels; plain keys descend one level.
				segs, err := SplitGroupPath(k.Value)
				if err != nil {
					return fmt.Errorf("in group %q: %v", group, err)
				}
				// Dotted keys are only group syntax at the TOP level of a
				// mapping; inside a group the same rule applies recursively.
				if err := walk(append(path, segs...), v); err != nil {
					return err
				}
			default:
				return fmt.Errorf("key %q inside group %q must map to a listener list or a nested group", k.Value, group)
			}
		}
		return nil
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "cors":
			_ = v.Decode(&config.CORS) // legacy inline block; split file overrides later
		case "log":
			_ = v.Decode(&config.Log)
		case "no_trailing_slash":
			if err := v.Decode(&config.NoTrailingSlash); err != nil {
				return fmt.Errorf("no_trailing_slash: %w", err)
			}
		default:
			switch v.Kind {
			case yaml.SequenceNode:
				if err := addEntry("", k.Value, v); err != nil {
					return err
				}
			case yaml.MappingNode:
				// Group: dotted top-level keys split into path segments.
				segs, err := SplitGroupPath(k.Value)
				if err != nil {
					return err
				}
				if err := walk(segs, v); err != nil {
					return err
				}
			default:
				return fmt.Errorf("key %q must map to a listener list or a group mapping", k.Value)
			}
		}
	}
	return nil
}

// orderedTopLevelKeys returns the document's top-level scalar keys in file
// order (generic; used by tests to assert key ordering of any YAML doc).
func orderedTopLevelKeys(data []byte) ([]string, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	mapping := node.Content[0]
	var keys []string
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Kind == yaml.ScalarNode {
			keys = append(keys, mapping.Content[i].Value)
		}
	}
	return keys, nil
}
