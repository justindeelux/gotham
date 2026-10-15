package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestServiceCannotClaimPanelHost(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{}).(*service)
	svc.guardDomains = func(_ context.Context, hosts ...string) (func(), error) {
		for _, host := range hosts {
			if host == "panel.example.com" {
				return nil, errors.New("reserved for the control plane")
			}
		}
		return func() {}, nil
	}
	server := repo.seedServer()
	environment, _ := repo.seedEnvironment()
	owner := uuid.New()
	input := CreateRequest{Name: "demo", ServerID: server, EnvironmentID: environment, ComposeYAML: testDocument, Env: map[string]string{"DOMAIN": "panel.example.com", "PASSWORD": "secret"}}
	if _, err := svc.Create(ctx, owner, input); !errors.Is(err, ErrValidation) {
		t.Fatalf("create=%v", err)
	}
	created := createService(t, svc, repo, owner)
	if _, err := svc.Update(ctx, owner, created.ID, UpdateRequest{Env: input.Env}); !errors.Is(err, ErrValidation) {
		t.Fatalf("env update=%v", err)
	}
	document := "services:\n  web:\n    image: nginx\n    labels:\n      gotham.domain: panel.example.com\n"
	if _, err := svc.Update(ctx, owner, created.ID, UpdateRequest{ComposeYAML: &document}); !errors.Is(err, ErrValidation) {
		t.Fatalf("compose update=%v", err)
	}
	stored, err := svc.Get(ctx, owner, created.ID)
	if err != nil || stored.Env["DOMAIN"] != "app.example.com" || stored.ComposeYAML != testDocument {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}
