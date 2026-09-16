package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

// defaultSessionTTL is how long an issued session token lives.
const defaultSessionTTL = 12 * time.Hour

type session struct {
	user         string
	fingerprint  string // credential fingerprint at issue time; mismatch invalidates
	expiresAt    time.Time
	lastActivity time.Time
}

// sessionStore holds opaque bearer tokens issued by Authenticate. Sessions
// bind to the user's CURRENT credential fingerprint: rotating the long-lived
// token (or deleting the user) invalidates every session issued before, with
// no explicit revocation list to maintain. Permissions are NOT snapshotted —
// authorization re-reads users.yaml on every request, so permission edits
// still apply immediately. Restarting the process clears all sessions (they
// are memory-only by design).
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	ttl      time.Duration
	users    *config.UserStore
}

func newSessionStore(users *config.UserStore, ttl time.Duration) *sessionStore {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return &sessionStore{
		sessions: make(map[string]*session),
		ttl:      ttl,
		users:    users,
	}
}

// issue creates a new session for user and returns the bearer token.
func (s *sessionStore) issue(user string) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token = hex.EncodeToString(raw)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(now)
	s.sessions[token] = &session{
		user:         user,
		fingerprint:  s.users.CredentialFingerprint(user),
		expiresAt:    now.Add(s.ttl),
		lastActivity: now,
	}
	return token, now.Add(s.ttl), nil
}

// lookup resolves a session token to its user, enforcing both expiry and the
// credential fingerprint. Expired/stale entries are removed on sight.
func (s *sessionStore) lookup(token string) (string, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(now)
	sess, ok := s.sessions[token]
	if !ok {
		return "", false
	}
	if now.After(sess.expiresAt) || s.users.CredentialFingerprint(sess.user) != sess.fingerprint {
		delete(s.sessions, token)
		return "", false
	}
	sess.lastActivity = now
	return sess.user, true
}

// revoke deletes one session (Logout).
func (s *sessionStore) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// gcLocked drops expired sessions; opportunistically caps runaway growth by
// also evicting the least-recently-used entries beyond a hard limit.
func (s *sessionStore) gcLocked(now time.Time) {
	for tok, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, tok)
		}
	}
	const maxSessions = 10000
	if len(s.sessions) <= maxSessions {
		return
	}
	// Evict oldest lastActivity until back under the cap.
	for len(s.sessions) > maxSessions {
		var oldestTok string
		var oldest time.Time
		first := true
		for tok, sess := range s.sessions {
			if first || sess.lastActivity.Before(oldest) {
				oldestTok, oldest, first = tok, sess.lastActivity, false
			}
		}
		delete(s.sessions, oldestTok)
	}
}

// fingerprintToken hashes a token for safe comparisons/logging.
func fingerprintToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}
