package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/server/ws"
)

func TestProjectSocketSurvivesJWTExpiry(t *testing.T) {
	for _, role := range []string{"owner", "member"} {
		t.Run(role, func(t *testing.T) {
			f := newAccessFixture(t)
			f.grant(t)
			identity := f.member
			if role == "owner" {
				identity = f.owner
			}
			claims, err := f.server.auth.VerifyAccessToken(identity.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			privateDER, err := x509.MarshalPKCS8PrivateKey(private)
			if err != nil {
				t.Fatal(err)
			}
			publicDER, err := x509.MarshalPKIXPublicKey(public)
			if err != nil {
				t.Fatal(err)
			}
			signer, err := auth.NewSigner(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
			if err != nil {
				t.Fatal(err)
			}
			f.server.auth = auth.New(f.store, signer, f.server.logger)
			claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(3 * time.Second))
			token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(private)
			if err != nil {
				t.Fatal(err)
			}
			live := httptest.NewServer(f.server.Handler())
			t.Cleanup(live.Close)
			address := "ws" + strings.TrimPrefix(live.URL, "http") + "/api/v1/ws?token=" + token + "&channel=tasks:" + f.projects[0].String()
			connection, err := websocket.Dial(address, "", live.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()
			var frame ws.Message
			for {
				if err := websocket.JSON.Receive(connection, &frame); err != nil {
					t.Fatal(err)
				}
				if frame.Type == ws.TypeSubscribed {
					break
				}
			}
			// Wait beyond exp and at least one full revalidation interval afterward.
			time.Sleep(time.Until(claims.ExpiresAt.Add(ws.AuthorizationInterval + 250*time.Millisecond)))
			if _, err := f.server.auth.VerifyAccessToken(token); err == nil {
				t.Fatal("test token has not expired")
			}
			if expired, err := websocket.Dial(address, "", live.URL); err == nil {
				_ = expired.Close()
				t.Fatal("expired JWT opened a new socket")
			}
			_ = connection.SetReadDeadline(time.Now().Add(ws.AuthorizationTimeout))
			if err := websocket.JSON.Send(connection, map[string]string{"subscribe": "tasks:" + f.projects[0].String()}); err != nil {
				t.Fatal(err)
			}
			if err := websocket.JSON.Receive(connection, &frame); err != nil || frame.Type != ws.TypeSubscribed {
				t.Fatalf("valid session cut at JWT expiry: %+v %v", frame, err)
			}
			if err := f.server.auth.Logout(context.Background(), identity.RefreshToken); err != nil {
				t.Fatal(err)
			}
			_ = connection.SetReadDeadline(time.Now().Add(ws.AuthorizationInterval + ws.AuthorizationTimeout + 250*time.Millisecond))
			if err := websocket.JSON.Receive(connection, &frame); err == nil {
				t.Fatalf("logged-out socket delivered %+v", frame)
			} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
				t.Fatalf("logout exceeded revocation bound: %v", err)
			}
		})
	}
}
