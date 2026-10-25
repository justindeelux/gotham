package requestreplay

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestKeyBoundsAndFingerprint(t *testing.T) {
	for _, key := range []string{"a", "A-_.:09", strings.Repeat("a", 128)} {
		if !ValidKey(key) {
			t.Errorf("rejected valid key %q", key)
		}
	}
	for _, key := range []string{"", "a b", "é", "a\n", strings.Repeat("a", 129)} {
		if ValidKey(key) {
			t.Errorf("accepted invalid key %q", key)
		}
	}
	digest := func(secret string, value any) string {
		r := &Request{}
		if err := Fingerprint(With(context.Background(), r), secret, value); err != nil {
			t.Fatal(err)
		}
		return r.Fingerprint
	}
	a := digest("stable", map[string]string{"b": "secret-password", "a": "one"})
	if a != digest("stable", map[string]string{"a": "one", "b": "secret-password"}) {
		t.Fatal("map ordering changes digest")
	}
	if a == digest("stable", map[string]string{"a": "one", "b": "different"}) || a == digest("rotated", map[string]string{"a": "one", "b": "secret-password"}) {
		t.Fatal("secret/key change did not change digest")
	}
	if strings.Contains(a, "password") {
		t.Fatal("plaintext leaked")
	}
}

func TestBeginAndSafeErrors(t *testing.T) {
	for _, values := range [][]string{{""}, {"a", "a"}, {"bad key"}} {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header["Idempotency-Key"] = values
		w := httptest.NewRecorder()
		if _, ok := Begin(w, req, uuid.New(), "create", uuid.Nil); ok || w.Code != 400 {
			t.Fatalf("header %v accepted", values)
		}
	}
	for _, err := range []error{ErrKey, ErrConflict, ErrExpired, ErrProgress, ErrMissing} {
		w := httptest.NewRecorder()
		if !WriteError(w, errors.Join(errors.New("secret payload"), err)) || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("unsafe error")
		}
	}
}
