package config

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// usersConfigFile holds control-API users. Unlike the other config files it is
// never watched: the UserStore below re-reads it lazily, so permission changes
// apply immediately without triggering an nginx reload.
const usersConfigFile = "users.yaml"

// Permission surfaces (config faces) and access modes for the control API.
const (
	SurfaceProxy = "proxy"
	SurfaceCORS  = "cors"
	SurfaceLogs  = "logs"
	SurfaceUsers = "users"

	ModeRead      = "read"
	ModeReadWrite = "read-write"
)

// User is one control-API identity from users.yaml.
//
// Authentication accepts either form; when both are present, Token wins:
//
//   - token_hash: sha256(token) as hex — the preferred, steady-state form;
//   - token:      plaintext token written by the operator. On a cold start
//     the app converts it to token_hash and strips the field (see
//     Editor.MigrateTokensToHashes); on hot reloads the file is left
//     untouched and the plaintext is used directly for verification.
type User struct {
	Name        string       `yaml:"name"`
	Token       string       `yaml:"token,omitempty"` // plaintext token; startup converts it to token_hash
	TokenHash   string       `yaml:"token_hash,omitempty"`
	Permissions []Permission `yaml:"permissions"`
}

// Permission grants access to one surface. An empty (or absent) selector list
// means unrestricted on that dimension; see internal/api/authz.go for the
// evaluation semantics.
type Permission struct {
	Surface   string   `yaml:"surface"`             // proxy | cors | logs | users
	Mode      string   `yaml:"mode"`                // read | read-write
	Domains   []string `yaml:"domains,omitempty"`   // exact or "*.suffix"; empty = unrestricted
	Upstreams []string `yaml:"upstreams,omitempty"` // exact proxy.yaml upstream_key; empty = unrestricted
}

type usersFile struct {
	Users []User `yaml:"users"`
}

// HashToken returns the sha256 hex digest used as token_hash.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// UserStore lazily loads users.yaml, caching by mtime+size so permission
// changes apply on the very next request without a watcher. It is safe for
// concurrent use. A missing or invalid file makes every lookup fail closed.
type UserStore struct {
	path string

	mu     sync.Mutex
	users  []User
	modT   time.Time
	size   int64
	loaded bool
}

// LoadUserStore returns a UserStore over <configDir>/users.yaml. The file is
// not read until the first Users/VerifyToken call.
func LoadUserStore(configDir string) *UserStore {
	return &UserStore{path: filepath.Join(configDir, usersConfigFile)}
}

// Users returns the current user list, reloading the file when its mtime or
// size changed. An unparseable file, or any permission with an unknown
// surface/mode, is an error: callers must fail closed.
func (s *UserStore) Users() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := os.Stat(s.path)
	if err != nil {
		return nil, fmt.Errorf("users file unavailable: %w", err)
	}
	if s.loaded && st.ModTime().Equal(s.modT) && st.Size() == s.size {
		return s.users, nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("users file unreadable: %w", err)
	}
	var uf usersFile
	if err := yaml.Unmarshal(data, &uf); err != nil {
		return nil, fmt.Errorf("users file invalid: %w", err)
	}
	if err := validateUsers(uf.Users); err != nil {
		return nil, err
	}

	s.users = uf.Users
	s.modT = st.ModTime()
	s.size = st.Size()
	s.loaded = true
	return s.users, nil
}

func validateUsers(users []User) error {
	for _, u := range users {
		if u.Name == "" {
			return errors.New("users file invalid: user with empty name")
		}
		if len(u.Permissions) == 0 {
			return fmt.Errorf("users file invalid: user %q has no permissions", u.Name)
		}
		for _, p := range u.Permissions {
			switch p.Surface {
			case SurfaceProxy, SurfaceCORS, SurfaceLogs, SurfaceUsers:
			default:
				return fmt.Errorf("users file invalid: user %q has unknown surface %q", u.Name, p.Surface)
			}
			switch p.Mode {
			case ModeRead, ModeReadWrite:
			default:
				return fmt.Errorf("users file invalid: user %q has unknown mode %q", u.Name, p.Mode)
			}
		}
	}
	return nil
}

// ErrUnknownToken is returned by VerifyToken when no user matches.
var ErrUnknownToken = errors.New("unknown token")

// VerifyToken resolves a bearer token to its user. A user carrying a
// plaintext token field is verified against it EXCLUSIVELY (it is the
// authoritative credential — a stale token_hash next to it never grants
// access); only users without a token field fall back to token_hash. All
// comparisons are constant-time; malformed credentials skip that user (fail
// closed for that user only).
func (s *UserStore) VerifyToken(token string) (*User, error) {
	users, err := s.Users()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(token))
	for i := range users {
		u := &users[i]
		if u.Token != "" {
			pw := sha256.Sum256([]byte(u.Token))
			if subtle.ConstantTimeCompare(pw[:], sum[:]) == 1 {
				return u, nil
			}
			continue // the token field is authoritative: no token_hash fallback
		}
		stored, err := hex.DecodeString(u.TokenHash)
		if err != nil || len(stored) != sha256.Size {
			continue
		}
		if subtle.ConstantTimeCompare(stored, sum[:]) == 1 {
			return u, nil
		}
	}
	return nil, ErrUnknownToken
}

// usersFileTemplate is the bootstrap users.yaml; hand-written (not yaml.Marshal)
// so it ships with explanatory comments.
const usersFileTemplate = `# sslly-nginx control API users.
# token_hash is sha256(token) as hex. Generate a new one with:
#   printf '%%s' 'your-secret-token' | shasum -a 256    # macOS
#   printf '%%s' 'your-secret-token' | sha256sum        # Linux
# Selector notes: domains entries are exact domains or "*.suffix" (subdomains
# only, not the bare apex); upstreams entries match proxy.yaml upstream keys
# verbatim. An absent selector means unrestricted on that dimension.
users:
  - name: admin
    token_hash: %s
    permissions:
      - surface: proxy
        mode: read-write
      - surface: cors
        mode: read-write
      - surface: logs
        mode: read-write
      - surface: users
        mode: read-write
`

// EnsureUsersFile bootstraps users.yaml with a full-access admin user when the
// file does not exist yet. The admin token is adminToken (already known to
// the operator) when non-empty, otherwise a fresh random 24-byte hex token
// that is returned so the caller can log it exactly once. Returns ("", nil)
// when the file already exists or was created from adminToken.
func EnsureUsersFile(configDir, adminToken string) (randomToken string, err error) {
	path := filepath.Join(configDir, usersConfigFile)
	if _, err := os.Stat(path); err == nil {
		return "", nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat users file: %w", err)
	}

	token := adminToken
	if token == "" {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate admin token: %w", err)
		}
		token = hex.EncodeToString(raw)
	}

	content := fmt.Sprintf(usersFileTemplate, HashToken(token))
	if err := os.WriteFile(path, []byte(content), 0666); err != nil {
		return "", fmt.Errorf("write users file: %w", err)
	}
	if adminToken != "" {
		return "", nil
	}
	return token, nil
}

// UsersFilePath returns the users.yaml path inside configDir (used by the
// editor for user mutations).
func UsersFilePath(configDir string) string {
	return filepath.Join(configDir, usersConfigFile)
}
