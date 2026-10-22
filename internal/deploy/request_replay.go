package deploy

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/requestreplay"
)

func (r *storeRepository) LookupRequestReceipt(ctx context.Context) error {
	return r.store.LookupRequestReceipt(ctx)
}
func (r *storeRepository) SaveRequestHook(ctx context.Context, outcome string) error {
	return r.store.SaveRequestHook(ctx, outcome)
}

func (s *Service) lookupReplay(ctx context.Context) error {
	if requestreplay.From(ctx) == nil {
		return nil
	}
	repo, ok := s.repo.(interface{ LookupRequestReceipt(context.Context) error })
	if !ok {
		return errors.New("deploy: replay persistence unavailable")
	}
	return repo.LookupRequestReceipt(ctx)
}

// SaveRequestHook records only a safe enum, never provider text or credentials.
func (s *Service) SaveRequestHook(ctx context.Context, outcome string) error {
	repo, ok := s.repo.(interface {
		SaveRequestHook(context.Context, string) error
	})
	if !ok {
		return errors.New("deploy: replay persistence unavailable")
	}
	return repo.SaveRequestHook(ctx, outcome)
}

func fingerprintApplication(ctx context.Context, secret string, app Application, password string, input CreateApplicationInput) error {
	if secret == "" {
		return errors.New("deploy: replay requires a stable secret key")
	}
	app.ID = uuid.Nil
	env := make([]EnvEntry, 0, len(input.Env))
	for _, entry := range input.Env {
		entry.Key = strings.TrimSpace(entry.Key)
		if strings.HasPrefix(entry.Value, secretRefPrefix) {
			entry.Value = secretRefPrefix + strings.TrimSpace(strings.TrimPrefix(entry.Value, secretRefPrefix))
		}
		env = append(env, entry)
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Key < env[j].Key })
	// A fixed placeholder canonicalizes derived managed paths before the real ID exists.
	storages, err := normalizeStorages(uuid.UUID{15: 1}, input.Storage)
	if err != nil {
		return err
	}
	sort.Slice(storages, func(i, j int) bool { return storages[i].Name < storages[j].Name })
	return requestreplay.Fingerprint(ctx, secret, struct {
		App      Application
		Password string
		Env      []EnvEntry
		Storage  []Storage
	}{app, password, env, storages})
}
