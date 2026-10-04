package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// sidOf extracts the session id bound to an access token.
func sidOf(t *testing.T, svc *Service, accessToken string) uuid.UUID {
	t.Helper()

	claims, err := svc.VerifyAccessToken(accessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.SessionID == "" {
		t.Fatal("access token carries no sid claim")
	}
	sid, err := uuid.Parse(claims.SessionID)
	if err != nil {
		t.Fatalf("parse sid: %v", err)
	}
	return sid
}

// liveIDs returns the ids in a session list.
func liveIDs(sessions []SessionInfo) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(sessions))
	for _, s := range sessions {
		out[s.ID] = true
	}
	return out
}

func TestServiceListSessionsMarksCurrent(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-list")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{UserAgent: "first-agent/1.0", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)
	second, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{UserAgent: "second-agent/1.0", IP: "10.0.0.2"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	current := sidOf(t, svc, second.AccessToken)

	sessions, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions returned %d rows, want 2", len(sessions))
	}
	byAgent := make(map[string]SessionInfo)
	for _, s := range sessions {
		byAgent[s.UserAgent] = s
		if s.IP == "" || s.CreatedAt.IsZero() || s.LastUsedAt.IsZero() {
			t.Errorf("session %+v missing metadata", s)
		}
	}
	if got := byAgent["first-agent/1.0"]; got.Current {
		t.Error("register session marked current, want only the login session")
	} else if got.IP != "10.0.0.1" {
		t.Errorf("register session IP = %q, want 10.0.0.1", got.IP)
	}
	if got := byAgent["second-agent/1.0"]; !got.Current {
		t.Error("login session not marked current")
	} else if got.IP != "10.0.0.2" {
		t.Errorf("login session IP = %q, want 10.0.0.2", got.IP)
	}
	if got := byAgent["second-agent/1.0"]; got.ID != current {
		t.Errorf("current session id = %v, want the login sid %v", got.ID, current)
	}
}

func TestServiceListSessionsSidLessMarksNone(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-sidless")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	sessions, err := svc.ListSessions(ctx, userID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d rows, want 1", len(sessions))
	}
	if sessions[0].Current {
		t.Error("sid-less token marked a session current, want none")
	}
	if sessions[0].UserAgent != "" || sessions[0].IP != "" {
		t.Errorf("zero meta rendered as %q/%q, want empty strings", sessions[0].UserAgent, sessions[0].IP)
	}
}

func TestServiceListSessionsExcludesDeadRows(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-dead")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)
	other, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// A revoked row (a rotated-away token) and an expired row must not list.
	rotated, err := svc.Refresh(ctx, other.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	_ = rotated
	current := sidOf(t, svc, rotated.AccessToken)

	if _, err := st.DB.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE refresh_hash = $1`, hashRefreshToken(first.RefreshToken)); err != nil {
		t.Fatalf("expire register session: %v", err)
	}

	sessions, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d rows, want only the live rotated session", len(sessions))
	}
	if sessions[0].ID != current {
		t.Errorf("listed session = %v, want the current %v", sessions[0].ID, current)
	}
	if !sessions[0].Current {
		t.Error("live session not marked current")
	}

	// Another user's sessions never list.
	svc.AllowOpenRegistration = true
	foreign, err := svc.Register(ctx, uniqueEmail("sessions-foreign"), "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register foreign: %v", err)
	}
	foreignID, _ := uuid.Parse(foreign.User.ID)
	foreignSessions, err := svc.ListSessions(ctx, foreignID, sidOf(t, svc, foreign.AccessToken))
	if err != nil {
		t.Fatalf("ListSessions foreign: %v", err)
	}
	if len(foreignSessions) != 1 || liveIDs(sessions)[foreignSessions[0].ID] {
		t.Error("foreign list leaks or misses rows")
	}
}

func TestServiceRotateUpdatesMeta(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-rotate")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{UserAgent: "old-agent", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	// Backdate so the rotation visibly moves last_used_at forward.
	if _, err := st.DB.Exec(ctx, `UPDATE sessions SET created_at = now() - interval '2 hours', last_used_at = now() - interval '2 hours' WHERE refresh_hash = $1`, hashRefreshToken(first.RefreshToken)); err != nil {
		t.Fatalf("backdate session: %v", err)
	}

	before, err := svc.ListSessions(ctx, userID, sidOf(t, svc, first.AccessToken))
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("ListSessions returned %d rows, want 1", len(before))
	}

	rotated, err := svc.Refresh(ctx, first.RefreshToken, SessionMeta{UserAgent: "new-agent/2.0", IP: "2001:db8::1"})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	after, err := svc.ListSessions(ctx, userID, sidOf(t, svc, rotated.AccessToken))
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("ListSessions returned %d rows, want 1", len(after))
	}
	got := after[0]
	if !got.Current {
		t.Error("rotated session not marked current")
	}
	// F3: the rotated row inherits the original sign-in created_at, while
	// last_used_at is the rotation time.
	if !got.CreatedAt.Equal(before[0].CreatedAt) {
		t.Errorf("created_at = %v, want the original sign-in time %v", got.CreatedAt, before[0].CreatedAt)
	}
	if got.UserAgent != "new-agent/2.0" {
		t.Errorf("user agent = %q, want the rotation value", got.UserAgent)
	}
	if got.IP != "2001:db8::1" {
		t.Errorf("IP = %q, want the rotation value", got.IP)
	}
	if !got.LastUsedAt.After(before[0].LastUsedAt) {
		t.Errorf("last_used_at = %v, want after %v", got.LastUsedAt, before[0].LastUsedAt)
	}
	if age := time.Since(got.LastUsedAt); age < 0 || age > 5*time.Minute {
		t.Errorf("last_used_at = %v, want ~now", got.LastUsedAt)
	}
}

func TestServiceSessionMetaTruncation(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-truncate")
	cleanupUser(t, st, email)
	// 300 runes plus invalid UTF-8: stored valid and capped at 256 chars.
	long := strings.Repeat("û", 300) + "\xff\xfe"
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{UserAgent: long, IP: "not-an-ip"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	sessions, err := svc.ListSessions(ctx, userID, sidOf(t, svc, first.AccessToken))
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d rows, want 1", len(sessions))
	}
	if n := len([]rune(sessions[0].UserAgent)); n != maxUserAgentLength {
		t.Errorf("stored user agent is %d chars, want %d", n, maxUserAgentLength)
	}
	if !utf8.ValidString(sessions[0].UserAgent) {
		t.Error("stored user agent is not valid UTF-8")
	}
	if sessions[0].IP != "" {
		t.Errorf("unparseable IP stored as %q, want unknown", sessions[0].IP)
	}
}

func TestServiceRevokeSession(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-revoke")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)
	second, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	third, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	current := sidOf(t, svc, third.AccessToken)

	sessions, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("ListSessions returned %d rows, want 3", len(sessions))
	}
	var victim uuid.UUID
	tokens := map[uuid.UUID]string{
		sidOf(t, svc, first.AccessToken):  first.RefreshToken,
		sidOf(t, svc, second.AccessToken): second.RefreshToken,
		sidOf(t, svc, third.AccessToken):  third.RefreshToken,
	}
	for _, s := range sessions {
		if s.ID != current && victim == uuid.Nil {
			victim = s.ID
		}
	}
	victimToken := tokens[victim]

	// Ending another session deletes its row. Replaying its refresh token is
	// then unknown: a plain 401 that leaves every other session alone (F1).
	if err := svc.RevokeSession(ctx, userID, victim); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := svc.Refresh(ctx, victimToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(revoked) error = %v, want ErrUnauthorized", err)
	}
	remaining, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := liveIDs(remaining); len(got) != 2 || got[victim] || !got[current] {
		t.Errorf("list after revoke = %v, want the victim gone and the rest live", got)
	}
	// The surviving sessions keep working: no family revoke happened.
	// Refreshing rotates the survivor, so re-read its sid afterwards.
	rotated, err := svc.Refresh(ctx, third.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh(survivor): %v, want the family intact", err)
	}
	newCurrent := sidOf(t, svc, rotated.AccessToken)

	// Already dead: 404-shaped.
	if err := svc.RevokeSession(ctx, userID, victim); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(dead) error = %v, want ErrNotFound", err)
	}
	// Unknown: 404-shaped.
	if err := svc.RevokeSession(ctx, userID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(unknown) error = %v, want ErrNotFound", err)
	}

	// Expired: 404-shaped (F4).
	expired, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	expiredID := sidOf(t, svc, expired.AccessToken)
	if _, err := st.DB.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE refresh_hash = $1`, hashRefreshToken(expired.RefreshToken)); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if err := svc.RevokeSession(ctx, userID, expiredID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(expired) error = %v, want ErrNotFound", err)
	}

	// Ending the caller's own session is allowed: its refresh chain dies
	// while the other survivor keeps working.
	if err := svc.RevokeSession(ctx, userID, newCurrent); err != nil {
		t.Fatalf("RevokeSession(current): %v", err)
	}
	if _, err := svc.Refresh(ctx, rotated.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(own revoked) error = %v, want ErrUnauthorized", err)
	}
	afterOwn, err := svc.ListSessions(ctx, userID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := liveIDs(afterOwn); len(got) != 1 || got[newCurrent] || got[victim] {
		t.Errorf("list after revoking own = %v, want only the unrevoked survivor", got)
	}
}

func TestServiceRevokeSessionForeignIsNotFound(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-revoke-own")
	cleanupUser(t, st, email)
	own, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	ownID, _ := uuid.Parse(own.User.ID)

	svc.AllowOpenRegistration = true
	foreign, err := svc.Register(ctx, uniqueEmail("sessions-revoke-foreign"), "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register foreign: %v", err)
	}
	foreignSessions, err := svc.ListSessions(ctx, uuid.MustParse(foreign.User.ID), sidOf(t, svc, foreign.AccessToken))
	if err != nil {
		t.Fatalf("ListSessions foreign: %v", err)
	}

	// Another user's id answers 404, never 403.
	if err := svc.RevokeSession(ctx, ownID, foreignSessions[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(foreign) error = %v, want ErrNotFound", err)
	}
	// The foreign session survived the probe.
	if _, err := svc.Refresh(ctx, foreign.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("Refresh(foreign after probe): %v", err)
	}
}

func TestServiceRevokeOtherSessions(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-revoke-others")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)
	second, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	third, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	current := sidOf(t, svc, third.AccessToken)

	if err := svc.RevokeOtherSessions(ctx, userID, current); err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}

	// Only the current session lists.
	remaining, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != current || !remaining[0].Current {
		t.Errorf("list after revoke-others = %+v, want only the current session", remaining)
	}

	// Replaying a deleted session is unknown: a plain 401, and the current
	// chain is untouched (F1). The current refresh comes first so its
	// rotation cannot be mistaken for damage from the replays below.
	refreshed, err := svc.Refresh(ctx, third.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh(current): %v", err)
	}
	for name, token := range map[string]string{"register": first.RefreshToken, "second": second.RefreshToken} {
		if _, err := svc.Refresh(ctx, token, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Refresh(%s) error = %v, want ErrUnauthorized", name, err)
		}
	}
	// The current chain survived the replays.
	if _, err := svc.Refresh(ctx, refreshed.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("Refresh(current after replays): %v, want the family intact", err)
	}
}

func TestServiceRevokeOtherSessionsStaleSid(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-stale-sid")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)
	second, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Rotate the login session: its old access token now carries a stale sid
	// (a revoked-but-present row).
	rotated, err := svc.Refresh(ctx, second.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	stale := sidOf(t, svc, second.AccessToken)
	fresh := sidOf(t, svc, rotated.AccessToken)

	// A stale sid refuses with 409 and deletes nothing (F2).
	if err := svc.RevokeOtherSessions(ctx, userID, stale); !errors.Is(err, ErrSessionUnknown) {
		t.Fatalf("RevokeOtherSessions(stale) error = %v, want ErrSessionUnknown", err)
	} else if err.Error() != "sign in again to manage other sessions" {
		t.Fatalf("message = %q, want the contract body", err.Error())
	}
	sessions, err := svc.ListSessions(ctx, userID, fresh)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("list after refused revoke-others = %d rows, want both live", len(sessions))
	}
	latestPair, err := svc.Refresh(ctx, rotated.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh(fresh after refused revoke-others): %v", err)
	}
	latest := sidOf(t, svc, latestPair.AccessToken)

	// A deleted session's sid also refuses: revoke the register session
	// outright, then present its access token's sid.
	registerSid := sidOf(t, svc, first.AccessToken)
	if err := svc.RevokeSession(ctx, userID, registerSid); err != nil {
		t.Fatalf("RevokeSession(register): %v", err)
	}
	if err := svc.RevokeOtherSessions(ctx, userID, registerSid); !errors.Is(err, ErrSessionUnknown) {
		t.Fatalf("RevokeOtherSessions(deleted sid) error = %v, want ErrSessionUnknown", err)
	}
	// Only the register session is gone; the rotated login session is live.
	sessions, err = svc.ListSessions(ctx, userID, latest)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != latest {
		t.Fatalf("list = %v, want only the fresh session", liveIDs(sessions))
	}

	// A foreign sid refuses and touches nothing on either account (F2).
	svc.AllowOpenRegistration = true
	foreign, err := svc.Register(ctx, uniqueEmail("sessions-stale-foreign"), "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register foreign: %v", err)
	}
	foreignID, _ := uuid.Parse(foreign.User.ID)
	foreignSid := sidOf(t, svc, foreign.AccessToken)
	if err := svc.RevokeOtherSessions(ctx, userID, foreignSid); !errors.Is(err, ErrSessionUnknown) {
		t.Fatalf("RevokeOtherSessions(foreign sid) error = %v, want ErrSessionUnknown", err)
	}
	sessions, err = svc.ListSessions(ctx, userID, latest)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("list after foreign-sid refusal = %d rows, want 1", len(sessions))
	}
	foreignSessions, err := svc.ListSessions(ctx, foreignID, foreignSid)
	if err != nil {
		t.Fatalf("ListSessions foreign: %v", err)
	}
	if len(foreignSessions) != 1 {
		t.Fatalf("foreign list = %d rows, want the foreign session untouched", len(foreignSessions))
	}
}

func TestServiceRevokeOtherSessionsSidLess(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-409")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	err = svc.RevokeOtherSessions(ctx, userID, uuid.Nil)
	if !errors.Is(err, ErrSessionUnknown) {
		t.Fatalf("RevokeOtherSessions(sid-less) error = %v, want ErrSessionUnknown", err)
	}
	if err.Error() != "sign in again to manage other sessions" {
		t.Fatalf("message = %q, want the contract body", err.Error())
	}
	// Nothing was revoked by the refused call.
	if _, err := svc.Refresh(ctx, first.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("Refresh(after refused revoke-others): %v", err)
	}
}

func TestServiceChangePasswordCapturesMeta(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-pwmeta")
	cleanupUser(t, st, email)
	const oldPassword = "s3cret-password"
	first, err := svc.Register(ctx, email, oldPassword, "", nil, SessionMeta{UserAgent: "old-agent", IP: "10.0.0.9"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	changed, err := svc.ChangePassword(ctx, userID, oldPassword, "new-s3cret-password", SessionMeta{UserAgent: "pw-agent", IP: "10.0.0.10"})
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	sessions, err := svc.ListSessions(ctx, userID, sidOf(t, svc, changed.AccessToken))
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d rows, want the caller's fresh session", len(sessions))
	}
	if sessions[0].UserAgent != "pw-agent" || sessions[0].IP != "10.0.0.10" {
		t.Errorf("fresh session meta = %q/%q, want the change-password values", sessions[0].UserAgent, sessions[0].IP)
	}
	if !sessions[0].Current {
		t.Error("fresh session not marked current")
	}
}

// TestServiceSessionMetadataMigrationBackfill: pre-PF-2 rows (NULL metadata)
// read back as empty strings with last_used_at set.
func TestServiceSessionMetadataMigrationBackfill(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("sessions-backfill")
	cleanupUser(t, st, email)
	first, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	var lastUsed, created time.Time
	if err := st.DB.QueryRow(ctx, `SELECT created_at, last_used_at FROM sessions WHERE refresh_hash = $1`, hashRefreshToken(first.RefreshToken)).Scan(&created, &lastUsed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("session row missing: %v", err)
		}
		t.Fatalf("read session row: %v", err)
	}
	if lastUsed.IsZero() {
		t.Error("last_used_at is zero, want now() at insert")
	}
}
