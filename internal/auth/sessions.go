package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxUserAgentLength = 256

type SessionMeta struct {
	UserAgent string
	IP        string
}

func cleanUserAgent(ua string) *string {
	ua = strings.ToValidUTF8(ua, "")
	ua = strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, ua)
	if utf8.RuneCountInString(ua) > maxUserAgentLength {
		ua = string([]rune(ua)[:maxUserAgentLength])
	}
	if ua == "" {
		return nil
	}
	return &ua
}

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

type SessionInfo struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

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

func (s *Service) SessionSignInAt(ctx context.Context, userID, sessionID uuid.UUID) (signInAt time.Time, found bool, err error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return time.Time{}, false, nil
	}
	signInAt, err = s.store.SessionSignInAt(ctx, pgUUID(sessionID), pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("auth: session sign-in: %w", err)
	}
	return signInAt, true, nil
}

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
