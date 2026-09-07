package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrEntryNotFound is returned by editor mutators when the requested key does
// not exist on disk; the API layer maps it to gRPC NotFound.
var ErrEntryNotFound = errors.New("entry not found")

// LogFieldPresence mirrors CORSFieldPresence for logs.yaml: a true flag means
// "write this field"; the rest keep their on-disk state.
type LogFieldPresence struct {
	SSLLYLevel      bool
	NginxLevel      bool
	NginxStderrAs   bool
	NginxStderrShow bool
}

// Editor performs surgical, comment- and order-preserving edits on the YAML
// config files. Every mutator returns the previous file bytes so callers can
// roll back with RestoreFile when the reload pipeline rejects the change.
// All writes go through WriteYAMLNodeFileAtomic (temp file + rename), so
// fsnotify never sees a half-written file.
//
// Comment contract: nodes that are not touched keep their comments and
// position; a replaced value loses only the comments attached to the removed
// items; deleted entries lose their comments. Blank-line spacing may be
// normalized by the yaml encoder — comment text and key order are what is
// preserved, not byte-identity.
type Editor struct {
	mu        sync.Mutex
	configDir string
}

// NewEditor returns an Editor over configDir.
func NewEditor(configDir string) *Editor {
	return &Editor{configDir: configDir}
}

func (e *Editor) path(name string) string { return filepath.Join(e.configDir, name) }

// loadDoc reads a YAML file and returns its previous bytes, the document
// node, and the root mapping node. An empty or null document yields a fresh
// mapping (cors.yaml and logs.yaml are legitimately created empty when no
// example exists).
func loadDoc(path string) (prev []byte, doc *yaml.Node, root *yaml.Node, err error) {
	prev, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	doc = &yaml.Node{}
	if err := yaml.Unmarshal(prev, doc); err != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if doc.Kind == 0 { // empty file: Unmarshal leaves a zero-value node
		doc.Kind = yaml.DocumentNode
	}
	root = documentRoot(doc)
	return prev, doc, root, nil
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = []*yaml.Node{root}
		return root
	}
	return doc.Content[0]
}

// findEntry locates a key in a mapping node by scalar value. YAML scalar
// values are unquoted, so "1234" and 1234 match identically.
func findEntry(mapping *yaml.Node, key string) (index int, keyNode, valNode *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i, mapping.Content[i], mapping.Content[i+1]
		}
	}
	return -1, nil, nil
}

// setMapLeaf replaces or appends a key/value pair inside a mapping node.
func setMapLeaf(mapping *yaml.Node, key string, val *yaml.Node) {
	if i, _, _ := findEntry(mapping, key); i >= 0 {
		mapping.Content[i+1] = val
		return
	}
	mapping.Content = append(mapping.Content, strScalar(key), val)
}

// --- node builders -------------------------------------------------------

func strScalar(s string) *yaml.Node {
	// No explicit tag: the encoder quotes only when the plain form would be
	// ambiguous (":", "*" ...), so numeric-looking keys like `9099:` stay
	// plain exactly like hand-written files. Empty strings get an explicit
	// !!str tag so they render as `""` instead of a null-looking bare value.
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: s}
	if s == "" {
		n.Tag = "!!str"
	}
	return n
}

func intScalar(i int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(i)}
}

func boolScalar(b bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}
}

// strSeq builds a block-style sequence; an empty list renders as flow `[]` so
// an explicit clear stays visible in the file.
func strSeq(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if len(items) == 0 {
		n.Style = yaml.FlowStyle
	}
	for _, it := range items {
		n.Content = append(n.Content, strScalar(it))
	}
	return n
}

// setSeqValue replaces the value under an existing key, reusing the sequence
// node object so its own comments survive; appends the pair when the key is
// absent.
func setSeqValue(root *yaml.Node, key string, items []string) {
	if i, _, val := findEntry(root, key); i >= 0 && val.Kind == yaml.SequenceNode {
		val.Content = strSeq(items).Content
	} else if i >= 0 {
		root.Content[i+1] = strSeq(items)
	} else {
		root.Content = append(root.Content, strScalar(key), strSeq(items))
	}
}

// mappingChild returns the mapping under key, replacing a wrong-kind value or
// appending the pair when missing.
func mappingChild(root *yaml.Node, key string) *yaml.Node {
	if i, _, val := findEntry(root, key); i >= 0 && val.Kind == yaml.MappingNode {
		return val
	} else if i >= 0 {
		child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content[i+1] = child
		return child
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, strScalar(key), child)
	return child
}

// seqChild returns the sequence under key (replacing a wrong-kind value or
// appending the pair when missing), so callers never duplicate the key.
func seqChild(root *yaml.Node, key string) *yaml.Node {
	if i, _, val := findEntry(root, key); i >= 0 && val.Kind == yaml.SequenceNode {
		return val
	} else if i >= 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content[i+1] = seq
		return seq
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, strScalar(key), seq)
	return seq
}

// --- proxy.yaml ----------------------------------------------------------

// SetProxyEntry creates or replaces one upstream entry (full replace of its
// listener list). New keys are appended at the end, keeping OrderedPorts
// stable and deterministic, exactly like a human appending an entry.
func (e *Editor) SetProxyEntry(upstreamKey string, listenerKeys []string) (prev []byte, err error) {
	if upstreamKey == "" {
		return nil, errors.New("upstream key must not be empty")
	}
	if len(listenerKeys) == 0 {
		return nil, fmt.Errorf("entry %q needs at least one listener key", upstreamKey)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	prev, doc, root, err := loadDoc(e.path(proxyConfigFile))
	if err != nil {
		return nil, err
	}
	setSeqValue(root, upstreamKey, listenerKeys)
	if err := WriteYAMLNodeFileAtomic(e.path(proxyConfigFile), doc); err != nil {
		return nil, err
	}
	return prev, nil
}

// DeleteProxyEntry removes an upstream entry. ErrEntryNotFound when absent.
func (e *Editor) DeleteProxyEntry(upstreamKey string) (prev []byte, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	prev, doc, root, err := loadDoc(e.path(proxyConfigFile))
	if err != nil {
		return nil, err
	}
	i, _, _ := findEntry(root, upstreamKey)
	if i < 0 {
		return nil, fmt.Errorf("%w: %s", ErrEntryNotFound, upstreamKey)
	}
	root.Content = append(root.Content[:i], root.Content[i+2:]...)
	if err := WriteYAMLNodeFileAtomic(e.path(proxyConfigFile), doc); err != nil {
		return nil, err
	}
	return prev, nil
}

// SetNoTrailingSlash fully replaces the no_trailing_slash list.
func (e *Editor) SetNoTrailingSlash(listenerKeys []string) (prev []byte, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	prev, doc, root, err := loadDoc(e.path(proxyConfigFile))
	if err != nil {
		return nil, err
	}
	setSeqValue(root, "no_trailing_slash", listenerKeys)
	if err := WriteYAMLNodeFileAtomic(e.path(proxyConfigFile), doc); err != nil {
		return nil, err
	}
	return prev, nil
}

// --- cors.yaml -----------------------------------------------------------

// SetCorsRule creates or updates one rule. Only fields flagged in presence
// are written; empty values render as "" / [] (explicit clear), reproducing
// cors.yaml presence semantics.
func (e *Editor) SetCorsRule(key string, cfg CORSConfig, presence CORSFieldPresence) (prev []byte, err error) {
	if key == "" {
		return nil, errors.New("cors rule key must not be empty")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	prev, doc, root, err := loadDoc(e.path(corsConfigFile))
	if err != nil {
		return nil, err
	}

	var rule *yaml.Node
	if i, _, val := findEntry(root, key); i >= 0 && val.Kind == yaml.MappingNode {
		rule = val
	} else if i >= 0 {
		rule = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content[i+1] = rule
	} else {
		rule = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, strScalar(key), rule)
	}

	if presence.AllowOrigin {
		setMapLeaf(rule, "allow_origin", strScalar(cfg.AllowOrigin))
	}
	if presence.AllowMethods {
		setMapLeaf(rule, "allow_methods", strSeq(cfg.AllowMethods))
	}
	if presence.AllowHeaders {
		setMapLeaf(rule, "allow_headers", strSeq(cfg.AllowHeaders))
	}
	if presence.ExposeHeaders {
		setMapLeaf(rule, "expose_headers", strSeq(cfg.ExposeHeaders))
	}
	if presence.MaxAge {
		setMapLeaf(rule, "max_age", intScalar(cfg.MaxAge))
	}
	if presence.AllowCredentials {
		setMapLeaf(rule, "allow_credentials", boolScalar(cfg.AllowCredentials))
	}

	if err := WriteYAMLNodeFileAtomic(e.path(corsConfigFile), doc); err != nil {
		return nil, err
	}
	return prev, nil
}

// DeleteCorsRule removes one rule by key. ErrEntryNotFound when absent.
func (e *Editor) DeleteCorsRule(key string) (prev []byte, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	prev, doc, root, err := loadDoc(e.path(corsConfigFile))
	if err != nil {
		return nil, err
	}
	i, _, _ := findEntry(root, key)
	if i < 0 {
		return nil, fmt.Errorf("%w: %s", ErrEntryNotFound, key)
	}
	root.Content = append(root.Content[:i], root.Content[i+2:]...)
	if err := WriteYAMLNodeFileAtomic(e.path(corsConfigFile), doc); err != nil {
		return nil, err
	}
	return prev, nil
}

// --- logs.yaml -----------------------------------------------------------

// UpdateLogsConfig writes the masked log fields, leaving the rest untouched.
func (e *Editor) UpdateLogsConfig(lc LogConfig, mask LogFieldPresence) (prev []byte, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	prev, doc, root, err := loadDoc(e.path(logsConfigFile))
	if err != nil {
		return nil, err
	}

	if mask.SSLLYLevel {
		setMapLeaf(mappingChild(root, "sslly"), "level", strScalar(lc.SSLLY.Level))
	}
	if mask.NginxLevel || mask.NginxStderrAs || mask.NginxStderrShow {
		nginx := mappingChild(root, "nginx")
		if mask.NginxLevel {
			setMapLeaf(nginx, "level", strScalar(lc.Nginx.Level))
		}
		if mask.NginxStderrAs {
			setMapLeaf(nginx, "stderr_as", strScalar(lc.Nginx.StderrAs))
		}
		if mask.NginxStderrShow {
			setMapLeaf(nginx, "stderr_show", strScalar(lc.Nginx.StderrShow))
		}
	}

	if err := WriteYAMLNodeFileAtomic(e.path(logsConfigFile), doc); err != nil {
		return nil, err
	}
	return prev, nil
}

// --- users.yaml ----------------------------------------------------------

// UpsertUser creates or replaces a user. An empty newTokenHash keeps the
// existing hash when the user already exists.
func (e *Editor) UpsertUser(u User, newTokenHash string) (prev []byte, err error) {
	if u.Name == "" {
		return nil, errors.New("user name must not be empty")
	}
	if newTokenHash != "" && len(newTokenHash) != 64 {
		return nil, errors.New("token hash must be 64 hex characters")
	}
	if err := validateUsers([]User{u}); err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	return e.upsertUserLocked(u, newTokenHash)
}

func (e *Editor) upsertUserLocked(u User, newTokenHash string) (prev []byte, err error) {
	doc, err := loadUsersDoc(e.path(usersConfigFile))
	if err != nil {
		return nil, err
	}
	prev = doc.prev
	root := documentRoot(doc.node)

	usersSeq := seqChild(root, "users")

	// Keep the existing hash when the caller did not supply a new token.
	if newTokenHash == "" {
		if _, item := findUserNode(usersSeq, u.Name); item != nil {
			if _, _, h := findEntry(item, "token_hash"); h != nil {
				u.TokenHash = h.Value
			}
			if _, _, p := findEntry(item, "token"); p != nil {
				u.Token = p.Value // keep token-style users working through API upserts
			}
		}
	} else {
		u.TokenHash = newTokenHash
	}

	node := userNode(u)
	if i, _ := findUserNode(usersSeq, u.Name); i >= 0 {
		usersSeq.Content[i] = node
	} else {
		usersSeq.Content = append(usersSeq.Content, node)
	}

	if err := WriteYAMLNodeFileAtomic(e.path(usersConfigFile), doc.node); err != nil {
		return nil, err
	}
	return prev, nil
}

// MigrateTokensToHashes converts every plaintext `token` field in
// users.yaml into `token_hash` (the token field wins over any pre-existing hash),
// strips the plaintext, and leaves a comment marking the conversion. This is
// a COLD-START-ONLY operation: the app calls it from Start(); hot reloads
// never touch the file and verify against the token field directly
// instead. Returns how many users were converted (0 = nothing written).
func (e *Editor) MigrateTokensToHashes() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	doc, err := loadUsersDoc(e.path(usersConfigFile))
	if err != nil {
		return 0, err
	}
	root := documentRoot(doc.node)
	_, _, usersVal := findEntry(root, "users")
	if usersVal == nil || usersVal.Kind != yaml.SequenceNode {
		return 0, nil
	}

	const note = "converted from token at startup"
	converted := 0
	for _, item := range usersVal.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		i, _, pwd := findEntry(item, "token")
		if i < 0 || pwd == nil || strings.TrimSpace(pwd.Value) == "" {
			continue
		}
		hash := HashToken(pwd.Value)

		// Strip the plaintext pair.
		item.Content = append(item.Content[:i], item.Content[i+2:]...)

		// Set the hash (replacing a stale one), annotated with the conversion.
		if j, key, _ := findEntry(item, "token_hash"); j >= 0 {
			item.Content[j+1].Value = hash
			key.LineComment = joinComments(key.LineComment, note)
		} else {
			key := strScalar("token_hash")
			key.LineComment = note
			item.Content = append(item.Content, key, strScalar(hash))
		}
		converted++
	}

	if converted > 0 {
		if err := WriteYAMLNodeFileAtomic(e.path(usersConfigFile), doc.node); err != nil {
			return 0, err
		}
	}
	return converted, nil
}

// DeleteUser removes a user by name. ErrEntryNotFound when absent.
func (e *Editor) DeleteUser(name string) (prev []byte, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	doc, err := loadUsersDoc(e.path(usersConfigFile))
	if err != nil {
		return nil, err
	}
	prev = doc.prev
	root := documentRoot(doc.node)

	_, _, usersVal := findEntry(root, "users")
	if usersVal == nil || usersVal.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%w: user %s", ErrEntryNotFound, name)
	}
	i, _ := findUserNode(usersVal, name)
	if i < 0 {
		return nil, fmt.Errorf("%w: user %s", ErrEntryNotFound, name)
	}
	usersVal.Content = append(usersVal.Content[:i], usersVal.Content[i+1:]...)

	if err := WriteYAMLNodeFileAtomic(e.path(usersConfigFile), doc.node); err != nil {
		return nil, err
	}
	return prev, nil
}

type usersDoc struct {
	prev []byte
	node *yaml.Node
}

func loadUsersDoc(path string) (*usersDoc, error) {
	prev, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read users.yaml: %w", err)
	}
	doc := &yaml.Node{}
	if err := yaml.Unmarshal(prev, doc); err != nil {
		return nil, fmt.Errorf("parse users.yaml: %w", err)
	}
	return &usersDoc{prev: prev, node: doc}, nil
}

func findUserNode(usersSeq *yaml.Node, name string) (index int, item *yaml.Node) {
	for i, it := range usersSeq.Content {
		if it.Kind != yaml.MappingNode {
			continue
		}
		if _, _, n := findEntry(it, "name"); n != nil && n.Value == name {
			return i, it
		}
	}
	return -1, nil
}

func userNode(u User) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMapLeaf(m, "name", strScalar(u.Name))
	setMapLeaf(m, "token_hash", strScalar(u.TokenHash))
	if u.Token != "" {
		// token-style user preserved verbatim (converted on next cold start).
		setMapLeaf(m, "token", strScalar(u.Token))
	}

	perms := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, p := range u.Permissions {
		pm := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMapLeaf(pm, "surface", strScalar(p.Surface))
		setMapLeaf(pm, "mode", strScalar(p.Mode))
		if len(p.Domains) > 0 {
			setMapLeaf(pm, "domains", strSeq(p.Domains))
		}
		if len(p.Upstreams) > 0 {
			setMapLeaf(pm, "upstreams", strSeq(p.Upstreams))
		}
		perms.Content = append(perms.Content, pm)
	}
	m.Content = append(m.Content, strScalar("permissions"), perms)
	return m
}

// --- rollback ------------------------------------------------------------

// RestoreFile writes previously captured bytes back, atomically.
func (e *Editor) RestoreFile(name string, prev []byte) error {
	path := e.path(name)
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(prev); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0666); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}
