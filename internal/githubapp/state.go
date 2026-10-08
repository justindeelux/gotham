package githubapp

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/google/uuid"
)

// stateTTL bounds how long a manifest or install state stays redeemable.
const stateTTL = 10 * time.Minute

// stateCapacity bounds outstanding states across all users, and statePerUser
// bounds one account's share, so a single owner cannot exhaust the store.
const (
	stateCapacity = 10000
	statePerUser  = 20
)

// stateKind names what a state authorizes: finishing the manifest callback or
// recording an installation.
type stateKind string

const (
	stateManifest stateKind = "manifest"
	stateInstall  stateKind = "install"
)

// stateEntry is one pending manifest/install authorization, bound to the user
// and (for installs) the app that issued it. States are single-use and
// expire after stateTTL.
type stateEntry struct {
	userID  uuid.UUID
	appID   uuid.UUID
	kind    stateKind
	expires time.Time
}

// stateStore keeps issued states in memory with no background goroutine:
// expired entries are swept on write and rejected on read.
type stateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
}

func newStateStore() *stateStore {
	return &stateStore{entries: make(map[string]stateEntry)}
}

// new issues a state for userID (and appID for installs). It fails when the
// caller already holds too many pending states.
func (s *stateStore) new(userID, appID uuid.UUID, kind stateKind) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if len(s.entries) >= stateCapacity {
		return "", ErrTooManyRequests
	}
	held := 0
	for _, e := range s.entries {
		if e.userID == userID {
			held++
		}
	}
	if held >= statePerUser {
		return "", ErrTooManyRequests
	}
	s.entries[state] = stateEntry{userID: userID, appID: appID, kind: kind, expires: time.Now().Add(stateTTL)}
	return state, nil
}

// redeem consumes a state: a second use, an expired state, or a state issued
// for another user, kind or app fails. Expiry is checked on read, so a state
// that lapsed between issue and callback is refused.
func (s *stateStore) redeem(state string, userID, appID uuid.UUID, kind stateKind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[state]
	if !ok {
		return false
	}
	delete(s.entries, state)
	if time.Now().After(entry.expires) {
		return false
	}
	if entry.userID != userID || entry.kind != kind {
		return false
	}
	if kind == stateInstall && entry.appID != appID {
		return false
	}
	return true
}

func (s *stateStore) sweepLocked() {
	now := time.Now()
	for state, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, state)
		}
	}
}
