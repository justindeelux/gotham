package githubapp

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSignAppJWTVerifies proves the JWT is RS256 over header.claims and
// verifies with the app public key.
func TestSignAppJWTVerifies(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	token, err := signAppJWT(123, pemBytes, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts, want 3", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("jwt does not verify: %v", err)
	}
	var claims map[string]any
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != float64(123) {
		t.Fatalf("iss = %v", claims["iss"])
	}
}

// TestSignAppJWTRejectsGarbage proves malformed keys fail closed.
func TestSignAppJWTRejectsGarbage(t *testing.T) {
	for _, pemBytes := range []string{"", "not-pem", "-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----"} {
		if _, err := signAppJWT(123, []byte(pemBytes), time.Now()); err == nil {
			t.Fatalf("key %q was accepted", redactedPEM([]byte(pemBytes)))
		}
	}
}

// signBody signs a delivery body the way GitHub does.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyDelivery(t *testing.T) {
	svc, repo, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	_, state, err := svc.InstallURL(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordInstallation(context.Background(), userID, app.ID, 999, state); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"action":"added","installation":{"id":999,"account":{"login":"acme"}}}`)
	header := http.Header{}
	header.Set("X-Hub-Signature-256", signBody("wh-secret", body))
	appID, ok := svc.VerifyDelivery(header, body)
	if !ok {
		t.Fatal("valid signature was rejected")
	}
	if appID != app.ID {
		t.Fatalf("verified app = %v, want %v", appID, app.ID)
	}

	// Wrong secret must fail.
	bad := http.Header{}
	bad.Set("X-Hub-Signature-256", signBody("wrong-secret", body))
	if _, ok := svc.VerifyDelivery(bad, body); ok {
		t.Fatal("wrong secret was accepted")
	}

	// Missing prefix must fail.
	bare := http.Header{}
	bare.Set("X-Hub-Signature-256", strings.TrimPrefix(signBody("wh-secret", body), "sha256="))
	if _, ok := svc.VerifyDelivery(bare, body); ok {
		t.Fatal("unprefixed signature was accepted")
	}

	// Unknown installation must fail.
	unknown := []byte(`{"action":"added","installation":{"id":4242,"account":{"login":"acme"}}}`)
	unknownHeader := http.Header{}
	unknownHeader.Set("X-Hub-Signature-256", signBody("wh-secret", unknown))
	if _, ok := svc.VerifyDelivery(unknownHeader, unknown); ok {
		t.Fatal("unknown installation was accepted")
	}
	_ = repo
}

// TestHandleAppEventRefreshesCache proves installation events refresh the
// repo cache and a deleted installation drops its row.
func TestHandleAppEventRefreshesCache(t *testing.T) {
	svc, repo, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	_, state, err := svc.InstallURL(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordInstallation(context.Background(), userID, app.ID, 999, state); err != nil {
		t.Fatal(err)
	}

	api.repos = []Repo{
		{ExternalID: "1", Name: "web", FullName: "acme/web", DefaultBranch: "main"},
		{ExternalID: "2", Name: "api", FullName: "acme/api", DefaultBranch: "main"},
	}
	body := []byte(`{"action":"added","installation":{"id":999,"account":{"login":"acme"}}}`)
	if err := svc.HandleAppEvent(context.Background(), app.ID, "installation_repositories", body); err != nil {
		t.Fatal(err)
	}
	cached, err := repo.ListRepoCache(context.Background(), app.ID, 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 2 {
		t.Fatalf("cache after event = %+v", cached)
	}

	deleted := []byte(`{"action":"deleted","installation":{"id":999,"account":{"login":"acme"}}}`)
	if err := svc.HandleAppEvent(context.Background(), app.ID, "installation", deleted); err != nil {
		t.Fatal(err)
	}
	insts, err := repo.ListInstallations(context.Background(), app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 0 {
		t.Fatalf("installations after delete = %+v", insts)
	}

	// Non-installation events are ignored.
	if err := svc.HandleAppEvent(context.Background(), app.ID, "push", body); err != nil {
		t.Fatal(err)
	}
	// Events for an app that does not own the installation are refused.
	if err := svc.HandleAppEvent(context.Background(), uuid.New(), "installation", deleted); err == nil {
		t.Fatal("foreign app event was accepted")
	}
}

// TestSecretsNeverLeak proves stored secrets never appear in API outputs or
// error text.
func TestSecretsNeverLeak(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	_ = app
	apps, err := svc.ListApps(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(apps)
	if strings.Contains(string(raw), "wh-secret") || strings.Contains(string(raw), "PRIVATE KEY") {
		t.Fatal("secrets appear in list output")
	}

	_, state, err := svc.InstallURL(context.Background(), userID, apps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordInstallation(context.Background(), uuid.New(), apps[0].ID, 999, state); err == nil {
		t.Fatal("expected error for wrong user")
	} else if strings.Contains(err.Error(), "wh-secret") || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatalf("error leaks secrets: %v", err)
	}
}
