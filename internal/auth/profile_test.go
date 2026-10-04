package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func strptr(s string) *string { return &s }

func TestServiceUpdateProfile(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("profile")
	cleanupUser(t, st, email)
	registered, err := svc.Register(ctx, email, "s3cret-password", "", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(registered.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	updated, err := svc.UpdateProfile(ctx, userID, strptr("Ada"))
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "Ada" {
		t.Fatalf("display name = %v, want Ada", updated.DisplayName)
	}

	me, err := svc.Me(ctx, userID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.DisplayName == nil || *me.DisplayName != "Ada" {
		t.Fatalf("Me display name = %v, want Ada", me.DisplayName)
	}
	if !me.HasPassword {
		t.Error("Me has_password = false, want true")
	}
	if !me.IsPlatformAdmin {
		t.Error("Me is_platform_admin = false, want true for the bootstrap account")
	}

	// The wire body carries the new fields and omits an unset display name.
	// The override opens the plain-insert path for the second account, which
	// is not the bootstrap admin.
	svc.AllowOpenRegistration = true
	second, err := svc.Register(ctx, uniqueEmail("profile-plain"), "s3cret-password", "", nil)
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}
	raw, err := json.Marshal(second.User)
	if err != nil {
		t.Fatalf("marshal user: %v", err)
	}
	for _, want := range []string{`"has_password":true`, `"is_platform_admin":false`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("user body %s missing %s", raw, want)
		}
	}
	if strings.Contains(string(raw), "display_name") {
		t.Errorf("user body %s carries display_name, want omitted when unset", raw)
	}
	withName, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal user: %v", err)
	}
	if !strings.Contains(string(withName), `"display_name":"Ada"`) {
		t.Errorf("user body %s missing display_name Ada", withName)
	}
}

func TestServiceUpdateProfileTrimBoundsClear(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("profile-bounds")
	cleanupUser(t, st, email)
	registered, err := svc.Register(ctx, email, "s3cret-password", "", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(registered.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	trimmed, err := svc.UpdateProfile(ctx, userID, strptr("  Ada Lovelace  "))
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if trimmed.DisplayName == nil || *trimmed.DisplayName != "Ada Lovelace" {
		t.Fatalf("display name = %v, want trimmed", trimmed.DisplayName)
	}

	if _, err := svc.UpdateProfile(ctx, userID, strptr(strings.Repeat("x", 64))); err != nil {
		t.Fatalf("64-char name: %v", err)
	}
	if _, err := svc.UpdateProfile(ctx, userID, strptr(strings.Repeat("x", 65))); !errors.Is(err, ErrDisplayNameInvalid) {
		t.Fatalf("65-char name error = %v, want ErrDisplayNameInvalid", err)
	}
	if err != nil && err.Error() != "display name must be 1-64 characters" {
		t.Fatalf("65-char message = %q, want the contract body", err.Error())
	}

	for _, clear := range []*string{nil, strptr(""), strptr("   ")} {
		cleared, err := svc.UpdateProfile(ctx, userID, clear)
		if err != nil {
			t.Fatalf("clear: %v", err)
		}
		if cleared.DisplayName != nil {
			t.Fatalf("display name = %q, want cleared", *cleared.DisplayName)
		}
	}

	if _, err := svc.UpdateProfile(ctx, uuid.New(), strptr("Ada")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("UpdateProfile(unknown) error = %v, want ErrUnauthorized", err)
	}
}

func TestServiceChangePassword(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("change")
	cleanupUser(t, st, email)
	const oldPassword = "s3cret-password"
	first, err := svc.Register(ctx, email, oldPassword, "", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(first.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}
	// A second session on another device that must die with the change.
	other, err := svc.Login(ctx, email, oldPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if _, err := svc.ChangePassword(ctx, userID, "wrong-password", "new-s3cret-password"); !errors.Is(err, ErrCurrentPasswordIncorrect) {
		t.Fatalf("wrong current error = %v, want ErrCurrentPasswordIncorrect", err)
	} else if err.Error() != "current password is incorrect" {
		t.Fatalf("wrong current message = %q, want the contract body", err.Error())
	}
	if _, err := svc.ChangePassword(ctx, userID, "", "new-s3cret-password"); !errors.Is(err, ErrCurrentPasswordIncorrect) {
		t.Fatalf("missing current error = %v, want ErrCurrentPasswordIncorrect", err)
	}
	if _, err := svc.ChangePassword(ctx, userID, oldPassword, "short"); !errors.Is(err, ErrValidation) {
		t.Fatalf("weak new error = %v, want ErrValidation", err)
	} else if want := ValidatePassword("short").Error(); err.Error() != want {
		t.Fatalf("weak new message = %q, want the register message %q", err.Error(), want)
	}

	changed, err := svc.ChangePassword(ctx, userID, oldPassword, "new-s3cret-password")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if changed.AccessToken == "" || changed.RefreshToken == "" {
		t.Fatal("ChangePassword returned an incomplete token pair")
	}
	if changed.User.ID != first.User.ID {
		t.Fatalf("changed user = %q, want %q", changed.User.ID, first.User.ID)
	}

	// Both pre-change sessions are dead; the returned pair works.
	for name, token := range map[string]string{"register": first.RefreshToken, "other": other.RefreshToken} {
		if _, err := svc.Refresh(ctx, token); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Refresh(%s session) error = %v, want ErrUnauthorized", name, err)
		}
	}
	if _, err := svc.Refresh(ctx, changed.RefreshToken); err != nil {
		t.Fatalf("Refresh(new pair): %v", err)
	}
	if _, err := svc.Login(ctx, email, oldPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, email, "new-s3cret-password"); err != nil {
		t.Fatalf("Login(new password): %v", err)
	}
	if _, err := svc.ChangePassword(ctx, uuid.New(), oldPassword, "another-s3cret"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ChangePassword(unknown) error = %v, want ErrUnauthorized", err)
	}
}

// TestServiceChangePasswordOAuthOnly: a passwordless (OAuth-created) account
// sets a password without a current one; a supplied current one is ignored.
func TestServiceChangePasswordOAuthOnly(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("oauth-only")
	cleanupUser(t, st, email)
	row, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(row.ID.Bytes)

	changed, err := svc.ChangePassword(ctx, userID, "whatever-supplied", "new-s3cret-password")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if !changed.User.HasPassword {
		t.Error("HasPassword = false after setting a password, want true")
	}
	if _, err := svc.Login(ctx, email, "new-s3cret-password"); err != nil {
		t.Fatalf("Login(new password): %v", err)
	}
}

// TestServiceChangePasswordRacesLogin: a login that verified the old password
// while a change commits must not mint a session under the new credential.
func TestServiceChangePasswordRacesLogin(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("change-race-login")
	cleanupUser(t, st, email)
	const oldPassword = "s3cret-password"
	first, err := svc.Register(ctx, email, oldPassword, "", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.afterPasswordVerified = func() {
		once.Do(func() { close(entered) })
		<-release
	}

	loginErr := make(chan error, 1)
	go func() {
		_, err := svc.Login(ctx, email, oldPassword)
		loginErr <- err
	}()

	// The login verified the old password and is parked before its
	// credential-version re-read; the change commits underneath it.
	<-entered
	if _, err := svc.ChangePassword(ctx, userID, oldPassword, "new-s3cret-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	close(release)

	if err := <-loginErr; !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("raced Login error = %v, want ErrInvalidCredentials", err)
	}
}

// TestServiceChangePasswordRacesRefresh: a refresh in flight while a change
// commits is refused; the old chain cannot mint a replacement.
func TestServiceChangePasswordRacesRefresh(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("change-race-refresh")
	cleanupUser(t, st, email)
	const oldPassword = "s3cret-password"
	first, err := svc.Register(ctx, email, oldPassword, "", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	release := make(chan struct{})
	svc.beforeRotate = func() { <-release }

	refreshErr := make(chan error, 1)
	go func() {
		_, err := svc.Refresh(ctx, first.RefreshToken)
		refreshErr <- err
	}()

	// Whichever interleaving wins, the old chain is refused: either the
	// change commits first and the read misses the deleted row, or the
	// refresh reads the live row and its rotation finds nothing to revoke.
	// The hook only parks the in-flight path so no goroutine leaks.
	if _, err := svc.ChangePassword(ctx, userID, oldPassword, "new-s3cret-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	close(release)

	if err := <-refreshErr; !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("raced Refresh error = %v, want ErrUnauthorized", err)
	}
}

// TestServiceChangePasswordConcurrent: two simultaneous changes with the same
// current password serialize; exactly one wins and the loser is told its
// current password is stale.
func TestServiceChangePasswordConcurrent(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := uniqueEmail("change-concurrent")
	cleanupUser(t, st, email)
	const oldPassword = "s3cret-password"
	first, err := svc.Register(ctx, email, oldPassword, "", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, _ := uuid.Parse(first.User.ID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.ChangePassword(ctx, userID, oldPassword, "concurrent-new-password")
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrCurrentPasswordIncorrect):
		default:
			t.Fatalf("concurrent change error = %v, want nil or ErrCurrentPasswordIncorrect", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent changes won %d, want exactly 1", wins)
	}
	if _, err := svc.Login(ctx, email, "concurrent-new-password"); err != nil {
		t.Fatalf("Login(winner password): %v", err)
	}
}
