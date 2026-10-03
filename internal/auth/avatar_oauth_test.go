package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSanitizeAvatarURL(t *testing.T) {
	valid := "https://avatars.githubusercontent.com/u/123456?v=4"
	if got, ok := SanitizeAvatarURL(valid); !ok || got != valid {
		t.Errorf("SanitizeAvatarURL(valid) = (%q, %v), want (%q, true)", got, ok, valid)
	}

	cases := map[string]string{
		"http scheme":        "http://avatars.githubusercontent.com/u/1",
		"foreign host":       "https://avatars.example/octo.png",
		"suffix attack host": "https://avatars.githubusercontent.com.evil.com/u/1",
		"prefix attack host": "https://evilavatars.githubusercontent.com/u/1",
		"userinfo":           "https://user@avatars.githubusercontent.com/u/1",
		"explicit port":      "https://avatars.githubusercontent.com:443/u/1",
		"data URL":           "data:image/png;base64,iVBORw0KGgo=",
		"relative URL":       "/u/1.png",
		"empty":              "",
		"oversized":          "https://avatars.githubusercontent.com/" + strings.Repeat("a", 512),
		"newline smuggled":   "https://avatars.githubusercontent.com/u/1\n",
		"control char":       "https://avatars.githubusercontent.com/u/\x7f1",
		"javascript scheme":  "javascript:alert(1)",
	}
	for name, raw := range cases {
		if got, ok := SanitizeAvatarURL(raw); ok {
			t.Errorf("SanitizeAvatarURL(%s) = (%q, true), want rejection", name, got)
		}
	}
}

// TestOAuthCallbackPersistsAvatarOnCreate proves the fix for the inert CSP
// change: the first OAuth login stores the provider avatar, so /me (and the
// header behind it) renders the image instead of the initial.
func TestOAuthCallbackPersistsAvatarOnCreate(t *testing.T) {
	const avatar = "https://avatars.githubusercontent.com/u/123456?v=4"
	provider := &fakeOAuthProvider{name: "github"}
	oauth, st := newTestOAuthWithStore(t, provider)
	oauth.auth.AllowOpenRegistration = true
	ctx := context.Background()

	email := uniqueEmail("oauth-avatar-new")
	cleanupUser(t, st, email)
	provider.identity = &OAuthIdentity{Email: email, Name: "Avatar New", AvatarURL: avatar}

	_, state, err := oauth.Begin(ctx, "github", "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	result, err := oauth.Callback(ctx, "github", "auth-code", state)
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if result.User.Avatar == nil || *result.User.Avatar != avatar {
		t.Errorf("Callback user avatar = %v, want %q", result.User.Avatar, avatar)
	}

	stored, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if stored.Avatar == nil || *stored.Avatar != avatar {
		t.Errorf("stored avatar = %v, want %q", stored.Avatar, avatar)
	}
}

// TestOAuthCallbackRefreshesAvatarOnLogin proves later logins refresh the
// stored avatar instead of leaving the first one stale.
func TestOAuthCallbackRefreshesAvatarOnLogin(t *testing.T) {
	const oldAvatar = "https://avatars.githubusercontent.com/u/111?v=4"
	const newAvatar = "https://avatars.githubusercontent.com/u/222?v=4"
	provider := &fakeOAuthProvider{name: "github"}
	oauth, st := newTestOAuthWithStore(t, provider)
	ctx := context.Background()

	email := uniqueEmail("oauth-avatar-refresh")
	cleanupUser(t, st, email)
	seeded, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := st.UpdateUserAvatar(ctx, seeded.ID, ptr(oldAvatar)); err != nil {
		t.Fatalf("UpdateUserAvatar: %v", err)
	}

	provider.identity = &OAuthIdentity{Email: email, AvatarURL: newAvatar}
	_, state, err := oauth.Begin(ctx, "github", "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	result, err := oauth.Callback(ctx, "github", "auth-code", state)
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if result.User.Avatar == nil || *result.User.Avatar != newAvatar {
		t.Errorf("Callback user avatar = %v, want %q", result.User.Avatar, newAvatar)
	}

	stored, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if stored.Avatar == nil || *stored.Avatar != newAvatar {
		t.Errorf("stored avatar = %v, want refreshed %q", stored.Avatar, newAvatar)
	}
}

// TestOAuthCallbackInvalidAvatarStoresNothing proves an invalid provider
// avatar never blocks the login and never lands in the users row.
func TestOAuthCallbackInvalidAvatarStoresNothing(t *testing.T) {
	invalid := map[string]string{
		"http scheme":  "http://avatars.githubusercontent.com/u/1",
		"foreign host": "https://evil.example/avatar.png",
		"userinfo":     "https://user@avatars.githubusercontent.com/u/1",
		"oversized":    "https://avatars.githubusercontent.com/" + strings.Repeat("a", 512),
		"control char": "https://avatars.githubusercontent.com/u/1\n",
		"empty":        "",
	}
	for name, avatar := range invalid {
		t.Run(name, func(t *testing.T) {
			provider := &fakeOAuthProvider{name: "github"}
			oauth, st := newTestOAuthWithStore(t, provider)
			oauth.auth.AllowOpenRegistration = true
			ctx := context.Background()

			email := uniqueEmail("oauth-avatar-bad")
			cleanupUser(t, st, email)
			provider.identity = &OAuthIdentity{Email: email, AvatarURL: avatar}

			_, state, err := oauth.Begin(ctx, "github", "")
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			result, err := oauth.Callback(ctx, "github", "auth-code", state)
			if err != nil {
				t.Fatalf("Callback with invalid avatar: %v (login must still succeed)", err)
			}
			if result.User.Avatar != nil {
				t.Errorf("Callback user avatar = %q, want nil", *result.User.Avatar)
			}

			stored, err := st.GetUserByEmail(ctx, email)
			if err != nil {
				t.Fatalf("GetUserByEmail: %v", err)
			}
			if stored.Avatar != nil {
				t.Errorf("stored avatar = %q, want NULL", *stored.Avatar)
			}
		})
	}
}

// TestOAuthCallbackInvalidAvatarKeepsExisting proves an invalid provider value
// on a later login leaves a previously stored avatar untouched.
func TestOAuthCallbackInvalidAvatarKeepsExisting(t *testing.T) {
	const stored = "https://avatars.githubusercontent.com/u/111?v=4"
	provider := &fakeOAuthProvider{name: "github"}
	oauth, st := newTestOAuthWithStore(t, provider)
	ctx := context.Background()

	email := uniqueEmail("oauth-avatar-keep")
	cleanupUser(t, st, email)
	seeded, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := st.UpdateUserAvatar(ctx, seeded.ID, ptr(stored)); err != nil {
		t.Fatalf("UpdateUserAvatar: %v", err)
	}

	provider.identity = &OAuthIdentity{Email: email, AvatarURL: "https://evil.example/x.png"}
	_, state, err := oauth.Begin(ctx, "github", "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	result, err := oauth.Callback(ctx, "github", "auth-code", state)
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if result.User.Avatar == nil || *result.User.Avatar != stored {
		t.Errorf("Callback user avatar = %v, want kept %q", result.User.Avatar, stored)
	}
}

// TestMeReturnsStoredAvatar proves /me exposes the stored avatar to the
// header behind it.
func TestMeReturnsStoredAvatar(t *testing.T) {
	const avatar = "https://avatars.githubusercontent.com/u/999?v=4"
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("oauth-avatar-me")
	cleanupUser(t, st, email)
	seeded, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := st.UpdateUserAvatar(ctx, seeded.ID, ptr(avatar)); err != nil {
		t.Fatalf("UpdateUserAvatar: %v", err)
	}

	user, err := svc.Me(ctx, uuid.UUID(seeded.ID.Bytes))
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if user.Avatar == nil || *user.Avatar != avatar {
		t.Errorf("Me avatar = %v, want %q", user.Avatar, avatar)
	}
}

func ptr(s string) *string { return &s }
