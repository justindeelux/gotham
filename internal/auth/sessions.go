package auth

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maxUserAgentLength bounds the stored User-Agent in characters (PF-2).
const maxUserAgentLength = 256

// SessionMeta carries the device metadata recorded with a refresh session at
// creation (login, register, OAuth, password change) and refreshed on every
// refresh-token rotation. The zero value means unknown: nothing is stored.
// There is no write per authenticated request; last_used_at moves on rotation
// only.
type SessionMeta struct {
	UserAgent string
	IP        string
}

// cleanUserAgent truncates the User-Agent to 256 characters and drops invalid
// UTF-8 rather than failing the login. Empty (or all-invalid) input stores
// NULL, which the list renders as "".
func cleanUserAgent(ua string) *string {
	ua = strings.ToValidUTF8(ua, "")
	if utf8.RuneCountInString(ua) > maxUserAgentLength {
		ua = string([]rune(ua)[:maxUserAgentLength])
	}
	if ua == "" {
		return nil
	}
	return &ua
}

// parseSessionIP parses the client IP for the inet column. Empty or
// unparseable input (a unix-socket peer, a test double) stores NULL, which
// the list renders as "". Zones cannot be stored in inet and are stripped;
// IPv4-mapped IPv6 addresses are unmapped to their IPv4 form.
func parseSessionIP(ip string) *netip.Addr {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	addr = addr.Unmap().WithZone("")
	return &addr
}

// SessionInfo is one live refresh session for the active-sessions list.
type SessionInfo struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

// ListSessions returns the caller's live sessions (not revoked, not expired),
// newest last use first, at most 50 rows. currentSessionID marks the caller's
// row; uuid.Nil (a token minted before PF-2) marks none rather than guessing.
func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]SessionInfo, error) {
	rows, err := s.store.ListSessionsByUser(ctx, pgUUID(userID), s.now())
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}

	out := make([]SessionInfo, 0, len(rows))
	for _, row := range rows {
		info := SessionInfo{
			ID:         uuid.UUID(row.ID.Bytes),
			CreatedAt:  row.CreatedAt.Time,
			LastUsedAt: row.LastUsedAt.Time,
			Current:    uuid.UUID(row.ID.Bytes) == currentSessionID,
		}
		if row.UserAgent != nil {
			info.UserAgent = *row.UserAgent
		}
		if row.Ip != nil {
			info.IP = row.Ip.String()
		}
		out = append(out, info)
	}
	return out, nil
}

// RevokeSession ends one of the caller's sessions by deleting its row, like
// logout does. It answers ErrNotFound when the id is unknown, belongs to
// another user, is already dead (revoked or deleted), or is expired (never a
// 403, so a probe cannot distinguish them). Ending the caller's own session
// is allowed; the web signs out afterwards.
//
// Deletion means a replayed refresh token is unknown: a plain 401 that can
// never trigger the reuse-detection family revoke, so revoking a stolen
// session cannot log the owner out everywhere (F1). Already-rotated rows stay
// untouched, preserving genuine-theft evidence. Access tokens already issued
// live out their 15-minute life (the same documented window as
// reset-password and the PF-1 password change).
func (s *Service) RevokeSession(ctx context.Context, userID, id uuid.UUID) error {
	deleted, err := s.store.DeleteSessionByID(ctx, pgUUID(id), pgUUID(userID))
	if err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	s.logger.Info("auth: session revoked", "user_id", userID)
	return nil
}

// RevokeOtherSessions ends every live session of the caller except the
// current one, by deleting those rows. The current session id must itself be
// a live session of the caller; a missing, stale (post-rotation), revoked,
// deleted, expired, or foreign sid answers ErrSessionUnknown ("sign in again
// to manage other sessions") and deletes nothing, rather than guessing which
// row is current (F2).
//
// Deletion means replayed refresh tokens are unknown: plain 401s that can
// never trigger the reuse-detection family revoke (F1). Access tokens already
// issued live out their 15-minute life (the same documented window as
// reset-password and the PF-1 password change).
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) error {
	if currentSessionID == uuid.Nil {
		return ErrSessionUnknown
	}
	_, currentLive, err := s.store.DeleteOtherSessionsGuarded(ctx, pgUUID(userID), pgUUID(currentSessionID))
	if err != nil {
		return fmt.Errorf("auth: revoke other sessions: %w", err)
	}
	if !currentLive {
		return ErrSessionUnknown
	}
	s.logger.Info("auth: other sessions revoked", "user_id", userID)
	return nil
}
