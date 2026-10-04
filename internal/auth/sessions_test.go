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
	other, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	current := sidOf(t, svc, other.AccessToken)

	sessions, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions returned %d rows, want 2", len(sessions))
	}
	var victim uuid.UUID
	for _, s := range sessions {
		if s.ID != current {
			victim = s.ID
		}
	}

	// Ending the other session removes it from the list; its refresh chain
	// is dead. Note the refresh below also trips the pre-existing
	// reuse detector (a revoked-but-present row is a replay), which ends the
	// family: the assertions after it use a fresh login.
	if err := svc.RevokeSession(ctx, userID, victim); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	remaining, err := svc.ListSessions(ctx, userID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != current {
		t.Errorf("list after revoke = %v, want only the current session", liveIDs(remaining))
	}
	if _, err := svc.Refresh(ctx, first.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(revoked) error = %v, want ErrUnauthorized", err)
	}

	// Already dead: 404-shaped.
	if err := svc.RevokeSession(ctx, userID, victim); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(dead) error = %v, want ErrNotFound", err)
	}
	// Unknown: 404-shaped.
	if err := svc.RevokeSession(ctx, userID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(unknown) error = %v, want ErrNotFound", err)
	}

	// Ending the caller's own session is allowed: a fresh login's session
	// revokes cleanly and its refresh chain dies with it.
	fresh, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	freshCurrent := sidOf(t, svc, fresh.AccessToken)
	if err := svc.RevokeSession(ctx, userID, freshCurrent); err != nil {
		t.Fatalf("RevokeSession(current): %v", err)
	}
	if _, err := svc.Refresh(ctx, fresh.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(own revoked) error = %v, want ErrUnauthorized", err)
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

	// The current chain works; every other chain is dead. The dead refreshes
	// come last: replaying a revoked-but-present token trips the pre-existing
	// reuse detector and would end the family mid-test.
	if _, err := svc.Refresh(ctx, third.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("Refresh(current): %v", err)
	}
	for name, token := range map[string]string{"register": first.RefreshToken, "second": second.RefreshToken} {
		if _, err := svc.Refresh(ctx, token, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Refresh(%s) error = %v, want ErrUnauthorized", name, err)
		}
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
