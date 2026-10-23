package githubapp

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/google/uuid"
)

const stateTTL = 10 * time.Minute

const (
	stateCapacity = 10000
	statePerUser  = 20
)

type stateKind string

const (
	stateManifest stateKind = "manifest"
	stateInstall  stateKind = "install"
)

type stateEntry struct {
	userID     uuid.UUID
	appID      uuid.UUID
	kind       stateKind
	baseURL    string
	apiBaseURL string
	expires    time.Time
}

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

func (s *stateStore) peek(state string, kind stateKind) (uuid.UUID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[state]
	if !ok || s.time().After(entry.expires) || entry.kind != kind {
		return uuid.Nil, false
	}
	return entry.userID, true
}

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
