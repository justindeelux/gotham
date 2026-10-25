package requestreplay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var (
	ErrKey      = errors.New("invalid_idempotency_key")
	ErrConflict = errors.New("request_conflict")
	ErrExpired  = errors.New("request_expired")
	ErrProgress = errors.New("request_in_progress")
	ErrMissing  = errors.New("request_target_missing")
)
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func ValidKey(key string) bool { return keyPattern.MatchString(key) }

type contextKey struct{}

type Request struct {
	Principal   uuid.UUID
	Operation   string
	Target      uuid.UUID
	Key         string
	Fingerprint string
	Result      *Result
}

type Result struct {
	ResourceID uuid.UUID `json:"resource_id"`
	RelatedID  uuid.UUID `json:"related_id,omitempty"`
	AcceptedAt time.Time `json:"accepted_at"`
	Replayed   bool      `json:"replayed"`
	Hook       string    `json:"webhook_outcome,omitempty"`
	Outcome    string    `json:"outcome"`
}

func From(ctx context.Context) *Request { value, _ := ctx.Value(contextKey{}).(*Request); return value }
func With(ctx context.Context, request *Request) context.Context {
	return context.WithValue(ctx, contextKey{}, request)
}
func ResultFrom(ctx context.Context) *Result {
	if r := From(ctx); r != nil {
		return r.Result
	}
	return nil
}
func Replayed(ctx context.Context) bool {
	result := ResultFrom(ctx)
	return result != nil && result.Replayed
}

func Begin(w http.ResponseWriter, r *http.Request, principal uuid.UUID, operation string, target uuid.UUID) (*http.Request, bool) {
	values, present := r.Header[http.CanonicalHeaderKey("Idempotency-Key")]
	if !present {
		return r, true
	}
	if len(values) != 1 || !ValidKey(values[0]) {
		WriteError(w, ErrKey)
		return r, false
	}
	return r.WithContext(With(r.Context(), &Request{Principal: principal, Operation: operation, Target: target, Key: values[0]})), true
}

func Fingerprint(ctx context.Context, secret string, input any) error {
	r := From(ctx)
	if r == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if secret == "" {
		sum := sha256.Sum256(data)
		r.Fingerprint = "v1:sha256:" + hex.EncodeToString(sum[:])
		return nil
	}
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte("gotham-request-replay-v1\x00"))
	_, _ = digest.Write(data)
	r.Fingerprint = "v1:hmac-sha256:" + hex.EncodeToString(digest.Sum(nil))
	return nil
}

func WriteError(w http.ResponseWriter, err error) bool {
	code, status := "", http.StatusConflict
	switch {
	case errors.Is(err, ErrKey):
		code, status = ErrKey.Error(), http.StatusBadRequest
	case errors.Is(err, ErrConflict):
		code = ErrConflict.Error()
	case errors.Is(err, ErrExpired):
		code = ErrExpired.Error()
	case errors.Is(err, ErrProgress):
		code = ErrProgress.Error()
		w.Header().Set("Retry-After", "2")
	case errors.Is(err, ErrMissing):
		code, status = ErrMissing.Error(), http.StatusNotFound
	default:
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}{code, code})
	return true
}

func WriteAccepted(w http.ResponseWriter, ctx context.Context, status int) bool {
	result := ResultFrom(ctx)
	if result == nil {
		return false
	}
	result.Outcome = "accepted_not_confirmed"
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Receipt *Result `json:"receipt"`
	}{result})
	return true
}

func RequireStrict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gotham-MCP-Contract") != "1" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(struct {
				Message string `json:"message"`
			}{"MCP contract required"})
			return
		}
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 || !ValidKey(keys[0]) {
			WriteError(w, ErrKey)
			return
		}
		next.ServeHTTP(w, r)
	})
}
