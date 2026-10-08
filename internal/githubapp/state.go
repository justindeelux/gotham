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
// and (for installs) the app that issued it. Manifest states also carry the
// GitHub base URLs chosen at StartManifest, so the callback exchanges the
// code against the same host (github.com or Enterprise). States are
// single-use and expire after stateTTL.
type stateEntry struct {
	userID     uuid.UUID
	appID      uuid.UUID
	kind       stateKind
	baseURL    string
	apiBaseURL string
	expires    time.Time
}

// stateStore keeps issued states in memory with no background goroutine:
// expired entries are swept on write and rejected on read. now overrides the
// clock in tests; it defaults to time.Now.
type stateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
	now     func() time.Time
}

func newStateStore() *stateStore {
	return &stateStore{entries: make(map[string]stateEntry), now: time.Now}
}

func (s *stateStore) time() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// new issues a state for userID (and appID for installs), remembering the
// base URLs for manifest states. It fails when the caller already holds too
// many pending states.
func (s *stateStore) new(userID, appID uuid.UUID, kind stateKind, baseURL, apiBaseURL string) (string, error) {
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
	s.entries[state] = stateEntry{
		userID:     userID,
		appID:      appID,
		kind:       kind,
		baseURL:    baseURL,
		apiBaseURL: apiBaseURL,
		expires:    s.time().Add(stateTTL),
	}
	return state, nil
}

// redeem consumes a state: a second use, an expired state, or a state issued
// for another user, kind or app fails. Expiry is checked on read, so a state
// that lapsed between issue and callback is refused. On success it returns
// the entry, carrying the manifest base URLs the callback must use.
func (s *stateStore) redeem(state string, userID, appID uuid.UUID, kind stateKind) (stateEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[state]
	if !ok {
		return stateEntry{}, false
	}
	delete(s.entries, state)
	if s.time().After(entry.expires) {
		return stateEntry{}, false
	}
	if entry.userID != userID || entry.kind != kind {
		return stateEntry{}, false
	}
	if kind == stateInstall && entry.appID != appID {
		return stateEntry{}, false
	}
	return entry, true
}

// peek reports the user a pending state was issued to, without consuming it.
// Expired states and kind mismatches fail.
func (s *stateStore) peek(state string, kind stateKind) (uuid.UUID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[state]
	if !ok || s.time().After(entry.expires) || entry.kind != kind {
		return uuid.Nil, false
	}
	return entry.userID, true
}

// peekApp reports the user and app a pending install state was issued for,
// without consuming it. Expired states and kind mismatches fail.
func (s *stateStore) peekApp(state string) (userID, appID uuid.UUID, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, found := s.entries[state]
	if !found || s.time().After(entry.expires) || entry.kind != stateInstall {
		return uuid.Nil, uuid.Nil, false
	}
	return entry.userID, entry.appID, true
}

func (s *stateStore) sweepLocked() {
	now := s.time()
	for state, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, state)
		}
	}
}
