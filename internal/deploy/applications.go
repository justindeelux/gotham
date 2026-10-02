package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/teams"
)

// secretRefPrefix marks an environment value as a sealed secret. Written by an
// API client the rest of the value is the plaintext to seal; read back from the
// API it is the reference (`secret:<secrets row id>`) of the sealed row. The
// prefix is what the FE EnvEditor renders as its "sealed secret" badge, and the
// plaintext never reaches a response body.
const secretRefPrefix = "secret:"

// EnvEntry is one row of an application's environment as the API sees it: a
// plain KEY=VALUE pair, or a key whose value carries secretRefPrefix and names
// a sealed secret instead of its contents.
type EnvEntry struct {
	Key   string
	Value string
}

// CreateApplicationInput is the validated creation payload (the wire shape in
// routes.go mirrors it one to one).
type CreateApplicationInput struct {
	Name       string
	Provider   string
	Repo       string
	CloneURL   string
	Branch     string
	BuildPack  string
	BaseDomain string
	Port       int32
	HostPort   int32
	ServerID   uuid.UUID
	Env        []EnvEntry
	Storage    []Storage
}

// UpdateApplicationInput carries the mutable application fields. Every field is
// optional (a zero value means "leave unchanged"); an empty ServerID clears the
// assignment so an application can be moved between nodes.
type UpdateApplicationInput struct {
	Name       *string
	Branch     *string
	BuildPack  *string
	BaseDomain *string
	Port       *int32
	HostPort   *int32
	ServerID   *uuid.UUID
}

// empty reports whether the update carries no field at all.
func (in UpdateApplicationInput) empty() bool {
	return in.Name == nil && in.Branch == nil && in.BuildPack == nil &&
		in.BaseDomain == nil && in.Port == nil && in.HostPort == nil &&
		in.ServerID == nil
}

// CreateApplication validates and stores a new application together with its
// environment and storage configuration. It answers only once every row of the
// payload is committed.
func (s *Service) CreateApplication(ctx context.Context, userID uuid.UUID, in CreateApplicationInput) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	app := Application{
		ID:         uuid.New(),
		UserID:     userID,
		TeamID:     teams.ScopeFor(ctx, userID).TeamID,
		Name:       strings.TrimSpace(in.Name),
		Provider:   strings.TrimSpace(in.Provider),
		Repo:       strings.TrimSpace(in.Repo),
		CloneURL:   strings.TrimSpace(in.CloneURL),
		Branch:     strings.TrimSpace(in.Branch),
		BuildPack:  strings.TrimSpace(in.BuildPack),
		BaseDomain: proxy.NormalizeDomain(in.BaseDomain),
		Port:       in.Port,
		HostPort:   in.HostPort,
		ServerID:   in.ServerID,
	}
	if app.Branch == "" {
		app.Branch = defaultBranch
	}
	if err := validateApplication(app, true); err != nil {
		return Application{}, err
	}
	if err := s.validateServer(ctx, userID, app.ServerID); err != nil {
		return Application{}, err
	}
	envVars, secrets, err := s.prepareEnv(ctx, uuid.Nil, in.Env)
	if err != nil {
		return Application{}, err
	}
	storages, err := normalizeStorages(app.ID, in.Storage)
	if err != nil {
		return Application{}, err
	}
	created, err := s.repo.CreateApplication(ctx, app, envVars, secrets, storages)
	if err != nil {
		return Application{}, err
	}
	// A new domain starts routing as soon as it is stored; the node may not
	// have Traefik yet, in which case the sync bootstraps it.
	if created.BaseDomain != "" {
		s.syncProxy(ctx, created)
	}
	return created, nil
}

// InstallHook installs the provider hook that triggers automatic deploys of
// a freshly created application (BE-4.4). It is deliberately best effort:
// the application row is already committed, so a provider outage, missing
// credentials or an unusable callback origin must not fail the create and
// leave the caller with an application it does not know exists. The call is
// bounded by Config.HookTimeout — a provider that accepts the connection and
// then stalls must not hold the create request past the SPA's own timeout —
// and a failure is logged with the configured logger (never silently
// dropped). The explicit idempotent POST /v1/applications/{id}/webhooks route
// is the retry path.
//
// attempted is false when no hook lifecycle is wired: there is no outcome to
// report and the create response omits the webhook field.
func (s *Service) InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) (attempted bool, err error) {
	lifecycle := s.hookLifecycle()
	if lifecycle == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.hookTimeout)
	defer cancel()
	err = lifecycle.InstallHook(ctx, userID, appID, r)
	if err != nil {
		s.logger.Warn("deploy: webhook not installed for the new application; retry with POST /v1/applications/{id}/webhooks",
			"application_id", appID, "error", err)
	}
	return true, err
}

// ListApplications returns the active team's applications, newest first.
// Without a team context it returns the creator's applications, which is the
// pre-teams behavior.
func (s *Service) ListApplications(ctx context.Context, userID uuid.UUID) ([]Application, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("deploy: repository is not configured")
	}
	applications, err := s.repo.ListApplications(ctx, teams.ScopeFor(ctx, userID))
	if err != nil {
		return nil, err
	}
	if applications == nil {
		return []Application{}, nil
	}
	return applications, nil
}

// GetApplication returns one application the caller owns. Another user's row
// answers ErrNotFound, so application IDs cannot be probed.
func (s *Service) GetApplication(ctx context.Context, userID, appID uuid.UUID) (Application, error) {
	return s.application(ctx, userID, appID, false)
}

// UpdateApplication applies a partial update to the mutable application fields
// (name, branch, build pack, domain, ports, server).
func (s *Service) UpdateApplication(ctx context.Context, userID, appID uuid.UUID, in UpdateApplicationInput) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	if in.empty() {
		return Application{}, fmt.Errorf("%w: no fields to update", ErrValidation)
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	// Serialize with deployment submission and manual control: a move may stop
	// the container on the previous node, and that must not race a deploy that
	// is selecting or retiring a target. Re-read under the lock so the update
	// is applied to the current row (no lost update from a stale snapshot).
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	// A move changes the node an in-flight deployment would target; refuse it
	// while a deployment is non-terminal, exactly like a manual control or a
	// delete. The worker does not take this lock, so the guard is what keeps a
	// move from stranding a container on the previous node.
	if in.ServerID != nil && *in.ServerID != app.ServerID {
		if err := s.rejectInFlight(ctx, app.ID); err != nil {
			return Application{}, err
		}
	}
	previousServer := app.ServerID
	previousDomain, previousPort, previousHostPort := app.BaseDomain, app.Port, app.HostPort
	if in.Name != nil {
		app.Name = strings.TrimSpace(*in.Name)
	}
	if in.Branch != nil {
		app.Branch = strings.TrimSpace(*in.Branch)
	}
	if in.BuildPack != nil {
		app.BuildPack = strings.TrimSpace(*in.BuildPack)
	}
	if in.BaseDomain != nil {
		next := proxy.NormalizeDomain(*in.BaseDomain)
		// Only an actual domain change resolves a migration-disabled binding:
		// a full-form update that resends the unchanged value (however
		// cased) must not silently reactivate a legacy conflict. The stored
		// value is normalized either way (BE-6.1 F6/F8).
		if next != proxy.NormalizeDomain(app.BaseDomain) {
			app.BaseDomainDisabled = false
		}
		app.BaseDomain = next
	}
	if in.Port != nil {
		app.Port = *in.Port
	}
	if in.HostPort != nil {
		app.HostPort = *in.HostPort
	}
	if in.ServerID != nil {
		if *in.ServerID != uuid.Nil {
			if err := s.validateServer(ctx, userID, *in.ServerID); err != nil {
				return Application{}, err
			}
		}
		app.ServerID = *in.ServerID
	}
	if app.Branch == "" {
		app.Branch = defaultBranch
	}
	// Legacy rows may predate normalization: normalize the resulting value so
	// an unrelated update (rename, branch) never fails on stored casing, even
	// with FEATURE_PROXY=false (BE-6.1 F8). An explicitly changed domain is
	// still validated strictly below.
	app.BaseDomain = proxy.NormalizeDomain(app.BaseDomain)
	// The clone URL is not part of the update payload, so it is left out of
	// validation: an application created with a development-local source must
	// still be renameable.
	if err := validateApplication(app, false); err != nil {
		return Application{}, err
	}
	updated, err := s.repo.UpdateApplication(ctx, app)
	if err != nil {
		return Application{}, err
	}
	// Routing input changed: refresh the hosting node's configuration. A node
	// whose route may have disappeared (a move, a changed domain, a cleared
	// domain) is refreshed too, so no stale route is left behind.
	moved := updated.ServerID != previousServer
	if moved {
		// The application is now bound to another node; stop the container it
		// left behind on the previous one (best effort — an unreachable node
		// must not fail the move). Deployments do not record their node, so the
		// newest recorded container is the one that ran there.
		s.stopContainerOnNode(ctx, previousServer, s.latestContainer(ctx, app.ID))
	}
	routingChanged := updated.BaseDomain != previousDomain || updated.Port != previousPort ||
		updated.HostPort != previousHostPort
	targets := make(map[uuid.UUID]bool, 2)
	if updated.BaseDomain != "" && (routingChanged || moved) {
		targets[updated.ServerID] = true
	}
	if previousDomain != "" && (moved || updated.BaseDomain != previousDomain) {
		targets[previousServer] = true
	}
	for serverID := range targets {
		s.syncProxyServer(ctx, serverID)
	}
	return updated, nil
}

// DeleteApplication removes an application together with everything that hangs
// off it. An application with a non-terminal deployment is refused with
// ErrConflict: deleting it would cascade the deployment row while the worker
// may be mid-run, orphaning a container no later deploy or delete can reach.
// Its provider hook is detached first and the delete fails closed on a
// removal failure, so nothing that would break the still-live application has
// been mutated when it aborts (the escape hatch for a hook whose provider is
// gone is ForgetWebhook, DELETE .../webhooks?force=true). The deploy key is
// detached next, best effort: once the hook is gone, a host failure must not
// leave a live application without automatic deploys (the local row cascades
// with the application and the warning names the remote key). The container
// removal stays best effort too: a control plane that cannot reach the node
// must still be able to delete an application.
func (s *Service) DeleteApplication(ctx context.Context, userID, appID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	// Serialize with deployment submission and manual control: the in-flight
	// check and the cascade must be atomic with respect to a new deployment.
	// Re-read under the lock so a concurrent move does not leave the delete
	// removing containers on the previous node and missing the current one.
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	if err := s.rejectInFlight(ctx, app.ID); err != nil {
		return err
	}
	// Preview siblings hang off this application outside the deploy schema
	// (preview_deploys is keyed by the base application and cascades); they
	// must be torn down before the base row disappears, or the cascade would
	// erase the only record of them. A cleanup failure aborts the delete: the
	// container stop inside the teardown is best effort, but the durable
	// binding must not be lost while a sibling still exists.
	if s.previewCleanup != nil {
		if err := s.previewCleanup(ctx, app.ID); err != nil {
			return err
		}
	}
	// Remove the provider hook (BE-4.4) FIRST, bounded like the create call,
	// and fail closed: the stored row is the only handle on the remote hook,
	// so deleting it while the host may still hold the hook would orphan the
	// hook with nothing left to identify it (the explicit route could no
	// longer reach it). Ordering matters for the still-live application too:
	// when this returns an error, the deploy key, the container and the row
	// are all untouched, so a webhook deploy that still reaches the app can
	// keep cloning. The caller retries, removes the hook on the host by hand,
	// or acknowledges the orphan with
	// DELETE /v1/applications/{id}/webhooks?force=true (ForgetWebhook) and
	// deletes again. A preview sibling never holds its own hook.
	if !app.IsPreview && supportedSourceProvider(app.Provider) && strings.TrimSpace(app.Repo) != "" {
		if lifecycle := s.hookLifecycle(); lifecycle != nil {
			hookCtx, cancel := context.WithTimeout(ctx, s.hookTimeout)
			err := lifecycle.RemoveHook(hookCtx, userID, app.ID)
			cancel()
			if err != nil {
				return err
			}
		}
	}
	// Detach the deploy key next. It is BEST EFFORT once the provider hook is
	// gone: aborting here would leave a live application whose automatic
	// deploys are silently disabled — the exact state this ordering exists to
	// prevent. The local key row cascades away with the application; a key
	// left registered on the Git host is the lesser evil, and the warning
	// names provider and repository so it can be removed there by hand. A
	// preview sibling reuses its base application's remote deploy key, so
	// only its local rows go and the remote key always stays registered.
	if app.IsPreview {
		if _, err := s.repo.DeleteDeployKey(ctx, app.ID); err != nil && !errors.Is(err, ErrNotFound) {
			s.logger.Warn("deploy: preview deploy key row could not be detached; the application still deletes",
				"application_id", app.ID, "error", err)
		}
	} else if err := s.detachDeployKey(ctx, app); err != nil {
		s.logger.Warn("deploy: deploy key could not be detached; the application still deletes",
			"application_id", app.ID, "provider", app.Provider, "repo", app.Repo, "error", err)
	}
	// Containers are removed before the row: the row is the only durable record
	// of the application's containers, so a row-delete failure after a
	// successful removal leaves no container behind (the accepted trade; the
	// caller retries the delete). A container left on a previous node by a move
	// is out of reach here — the move stopped it best effort and deployments do
	// not record their node (see the report).
	s.removeApplicationContainers(ctx, app)
	if err := s.repo.DeleteApplication(ctx, appID); err != nil {
		return err
	}
	// The application row is gone: regenerate the node's configuration so the
	// deleted route stops being served immediately. An application without a
	// domain never had a route to remove.
	if app.BaseDomain != "" {
		s.syncProxyServer(ctx, app.ServerID)
	}
	return nil
}

// GetEnv returns the application's environment: plain values verbatim and
// sealed secrets as `secret:<id>` references. Plaintext is never returned.
func (s *Service) GetEnv(ctx context.Context, userID, appID uuid.UUID) ([]EnvEntry, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	return s.envEntries(ctx, appID)
}

// ReplaceEnv rewrites the whole environment collection and returns it as
// stored. A `secret:` value that still points at a stored secret keeps its
// ciphertext; any other `secret:` value is sealed as new plaintext.
func (s *Service) ReplaceEnv(ctx context.Context, userID, appID uuid.UUID, entries []EnvEntry) ([]EnvEntry, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	if _, err := s.application(ctx, userID, appID, true); err != nil {
		return nil, err
	}
	envVars, secrets, err := s.prepareEnv(ctx, appID, entries)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceEnvVars(ctx, appID, envVars, secrets); err != nil {
		return nil, err
	}
	return s.envEntries(ctx, appID)
}

// GetStorages returns the application's storage mappings.
func (s *Service) GetStorages(ctx context.Context, userID, appID uuid.UUID) ([]Storage, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	storages, err := s.repo.ListStorages(ctx, appID)
	if err != nil {
		return nil, err
	}
	if storages == nil {
		return []Storage{}, nil
	}
	return storages, nil
}

// ReplaceStorages rewrites the whole storage collection and returns it as
// stored.
func (s *Service) ReplaceStorages(ctx context.Context, userID, appID uuid.UUID, storages []Storage) ([]Storage, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	if _, err := s.application(ctx, userID, appID, true); err != nil {
		return nil, err
	}
	normalized, err := normalizeStorages(appID, storages)
	if err != nil {
		return nil, err
	}
	s.warnStorageChanges(ctx, appID, storages, normalized)
	if err := s.repo.ReplaceStorages(ctx, appID, normalized); err != nil {
		return nil, err
	}
	return s.GetStorages(ctx, userID, appID)
}

// warnStorageChanges logs when a save changes the resolved host source of an
// existing storage row in a way that strands data: a previously explicit host
// path blanked to a managed path, or a pre-change bare named volume silently
// re-namespaced. An intentional explicit change is left to the operator, and a
// re-save that resolves to the same value (the derived managed path) does not
// warn.
func (s *Service) warnStorageChanges(ctx context.Context, appID uuid.UUID, input, normalized []Storage) {
	existing, err := s.repo.ListStorages(ctx, appID)
	if err != nil {
		return
	}
	byName := make(map[string]Storage, len(existing))
	for _, row := range existing {
		byName[row.Name] = row
	}
	for index, row := range normalized {
		previous, ok := byName[row.Name]
		if !ok || strings.TrimSpace(previous.HostPath) == "" || previous.HostPath == row.HostPath {
			continue
		}
		if !strings.HasPrefix(previous.HostPath, "/") {
			s.logger.Warn("deploy: storage named volume renamed; the old volume's data is not moved",
				"application_id", appID, "storage", row.Name,
				"previous_volume", previous.HostPath, "new_volume", row.HostPath)
			continue
		}
		if index < len(input) && strings.TrimSpace(input[index].HostPath) == "" {
			s.logger.Warn("deploy: storage host path blanked; existing data is not moved to the managed path",
				"application_id", appID, "storage", row.Name,
				"previous_host_path", previous.HostPath, "new_host_path", row.HostPath)
		}
	}
}

// Stop stops the container of the application's newest deployment.
func (s *Service) Stop(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	return s.controlContainer(ctx, userID, appID, false)
}

// Start restarts the container of the application's newest deployment.
func (s *Service) Start(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	return s.controlContainer(ctx, userID, appID, true)
}

// controlContainer runs one manual container operation through the node seam.
// The deployment row is left untouched: it identifies the release, and its
// state must keep describing that release (a stop would otherwise break
// rollback target selection). The agent call is bounded by ControlTimeout; a
// call that exhausts the bound answers ErrAgentUnavailable, which the client
// may safely retry (start/stop are idempotent).
func (s *Service) controlContainer(ctx context.Context, userID, appID uuid.UUID, start bool) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	// Hold the application lock across target selection and the agent call: a
	// concurrent deploy must not retire the selected container between the
	// in-flight check and a delayed start/stop, which would restart a retired
	// release (leaving two running). Re-read under the lock so a concurrent
	// move cannot leave the manual call acting on the previous node.
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	target, err := s.controlTarget(ctx, app)
	if err != nil {
		return Deployment{}, err
	}
	// Bound the agent call so a hung agent cannot pin the application lock and
	// stall every deploy, move and delete for this application.
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	node, err := s.dialNode(callCtx, app.ServerID)
	if err != nil {
		return Deployment{}, err
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "deployment_id", target.ID, "error", err)
		}
	}()
	if start {
		err = node.Start(callCtx, target.ContainerID)
	} else {
		err = node.Stop(callCtx, target.ContainerID)
	}
	if err != nil {
		return Deployment{}, err
	}
	return target, nil
}

// controlTarget picks the container a manual stop/start acts on: the newest
// deployment that started one. An in-flight deployment answers ErrConflict (its
// container is being replaced right now), and an application that never
// started a container answers ErrNotFound.
func (s *Service) controlTarget(ctx context.Context, app Application) (Deployment, error) {
	if app.ServerID == uuid.Nil {
		return Deployment{}, ErrServerNotFound
	}
	deployments, err := s.repo.ListDeployments(ctx, app.ID)
	if err != nil {
		return Deployment{}, err
	}
	for _, dep := range deployments { // newest first
		if !dep.State.Terminal() {
			return Deployment{}, fmt.Errorf("%w: a deployment is in progress", ErrConflict)
		}
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			return dep, nil
		}
	}
	return Deployment{}, fmt.Errorf("%w: application has no running container", ErrNotFound)
}

// rejectInFlight returns ErrConflict when the application has a non-terminal
// deployment: its container is being replaced right now, so a manual control
// or a delete must wait for it to finish. It is the delete-side mirror of
// controlTarget's guard.
func (s *Service) rejectInFlight(ctx context.Context, appID uuid.UUID) error {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		return err
	}
	for _, dep := range deployments { // newest first
		if !dep.State.Terminal() {
			return fmt.Errorf("%w: a deployment is in progress", ErrConflict)
		}
	}
	return nil
}

// latestContainer returns the newest container any deployment of the
// application started ("" when there is none).
func (s *Service) latestContainer(ctx context.Context, appID uuid.UUID) string {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		s.logger.Warn("deploy: lookup container failed", "application_id", appID, "error", err)
		return ""
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			return dep.ContainerID
		}
	}
	return ""
}

// containerCleanupTimeout bounds one best-effort container operation on a node
// that may have stopped answering. It is a safety net so a hung node cannot pin
// the application lock; it is not a retry budget.
const containerCleanupTimeout = 15 * time.Second

// stopContainerOnNode stops one container on one node, swallowing every
// failure: a move must not depend on the previous node being reachable.
func (s *Service) stopContainerOnNode(ctx context.Context, serverID uuid.UUID, containerID string) {
	if serverID == uuid.Nil || containerID == "" || s.dial == nil {
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerCleanupTimeout)
	defer cancel()
	node, err := s.dialNode(opCtx, serverID)
	if err != nil {
		s.logger.Warn("deploy: best-effort stop skipped", "server_id", serverID, "error", err)
		return
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "error", err)
		}
	}()
	if err := node.Stop(opCtx, containerID); err != nil {
		s.logger.Warn("deploy: best-effort stop failed", "container_id", containerID, "error", err)
	}
}

// removeApplicationContainers removes every container of an application from
// its node, swallowing every failure: deletion must not depend on the node
// being reachable. Containers are found by the gotham.app_id label so a
// container orphaned by a failed deploy or a lost response is reached too; the
// recorded deployment container IDs are removed as well, which also covers a
// node that cannot list containers. Rollback images and persistent bind
// directories survive (the agent's Remove removes only the container and its
// anonymous volumes).
func (s *Service) removeApplicationContainers(ctx context.Context, app Application) {
	if app.ServerID == uuid.Nil || s.dial == nil {
		return
	}
	// Delete spells no requirement on the node answering; bound every call so a
	// hung node cannot pin the application lock.
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerCleanupTimeout)
	defer cancel()
	targets := make(map[string]bool, 2)
	deployments, err := s.repo.ListDeployments(opCtx, app.ID)
	if err != nil {
		s.logger.Warn("deploy: lookup containers for delete failed", "application_id", app.ID, "error", err)
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			targets[dep.ContainerID] = true
		}
	}

	node, err := s.dialNode(opCtx, app.ServerID)
	if err != nil {
		s.logger.Warn("deploy: best-effort container removal skipped", "application_id", app.ID, "error", err)
		return
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "error", err)
		}
	}()
	containers, listErr := node.Containers(opCtx)
	if listErr != nil {
		s.logger.Warn("deploy: container listing failed during delete; removing recorded containers only",
			"application_id", app.ID, "error", listErr)
	} else {
		for _, candidate := range containers {
			if candidate.GetLabels()[labelAppID] == app.ID.String() {
				targets[candidate.GetId()] = true
			}
		}
	}
	for id := range targets {
		if err := node.Remove(opCtx, id); err != nil {
			s.logger.Warn("deploy: best-effort container removal failed",
				"application_id", app.ID, "container_id", id, "error", err)
		}
	}
}

// validateServer enforces that the application points at a server the control
// plane knows AND that the caller's active team may target it. A node of
// another team fails like a missing one (ErrServerNotFound, 404), so node IDs
// cannot be probed and an application can never be bound to a foreign team's
// node (F6). A legacy node without a team stays shared.
func (s *Service) validateServer(ctx context.Context, userID, serverID uuid.UUID) error {
	if serverID == uuid.Nil {
		return fmt.Errorf("%w: server is required", ErrValidation)
	}
	known, err := s.repo.ServerExists(ctx, serverID, teams.ScopeFor(ctx, userID))
	if err != nil {
		return err
	}
	if !known {
		return ErrServerNotFound
	}
	return nil
}

// validateApplication checks the fields every write path shares: a name, a
// cloneable source (creation only), a build pack builds.ParseEngineKind
// accepts ("" selects auto-detection) and ports inside the 0–65535 range the
// schema CHECKs enforce.
func validateApplication(app Application, checkSource bool) error {
	if app.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if checkSource {
		if err := validateCloneURL(app.CloneURL); err != nil {
			return err
		}
	}
	if _, err := builds.ParseEngineKind(app.BuildPack); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if app.BaseDomain != "" {
		// The domain ends up inside a Traefik Host() rule, so it is validated
		// at write time: an injection attempt or a malformed hostname must
		// never reach the generator.
		if err := proxy.ValidateDomain(app.BaseDomain); err != nil {
			return fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}
	if err := validatePort("port", app.Port); err != nil {
		return err
	}
	return validatePort("host port", app.HostPort)
}

// validatePort rejects values the applications table CHECKs would refuse, so a
// bad port is a 400 instead of a database error.
func validatePort(field string, port int32) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("%w: %s must be between 0 and 65535", ErrValidation, field)
	}
	return nil
}

// prepareEnv turns API entries into the two collections that are stored: plain
// env vars and sealed secrets. Values carrying secretRefPrefix are sealed with
// providers.SealSecret — unless they still reference a secret this application
// already holds, in which case the ciphertext is re-written unchanged so a
// configuration round-trip cannot re-seal a reference as if it were plaintext.
func (s *Service) prepareEnv(ctx context.Context, appID uuid.UUID, entries []EnvEntry) ([]EnvVar, []Secret, error) {
	existing, err := s.repo.ListSecrets(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	byReference := make(map[string]Secret, len(existing))
	for _, secret := range existing {
		byReference[secret.ID.String()] = secret
	}

	envVars := make([]EnvVar, 0, len(entries))
	secrets := make([]Secret, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if err := validateEnvKey(key); err != nil {
			return nil, nil, err
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("%w: duplicate environment key %q", ErrValidation, key)
		}
		seen[key] = true

		if !strings.HasPrefix(entry.Value, secretRefPrefix) {
			envVars = append(envVars, EnvVar{Key: key, Value: entry.Value})
			continue
		}
		reference := strings.TrimSpace(strings.TrimPrefix(entry.Value, secretRefPrefix))
		if reference == "" {
			return nil, nil, fmt.Errorf("%w: secret %q has no value", ErrValidation, key)
		}
		if stored, ok := byReference[reference]; ok && stored.Key == key {
			secrets = append(secrets, Secret{
				ID:         stored.ID,
				Key:        key,
				Ciphertext: stored.Ciphertext,
			})
			continue
		}
		// A reference the application does not hold is a stale or invented one:
		// re-sealing it would silently replace the secret with a UUID.
		if _, err := uuid.Parse(reference); err == nil {
			return nil, nil, fmt.Errorf("%w: unknown secret reference for %q", ErrValidation, key)
		}
		ciphertext, err := providers.SealSecret(s.secret, reference)
		if err != nil {
			return nil, nil, fmt.Errorf("deploy: seal secret %s: %w", key, err)
		}
		secrets = append(secrets, Secret{ID: uuid.New(), Key: key, Ciphertext: ciphertext})
	}
	return envVars, secrets, nil
}

// validateEnvKey accepts any structurally possible container variable name.
// The FE warns (without blocking) on ^[A-Z][A-Z0-9_]*$; the API only rejects
// what Docker could not pass through.
func validateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: environment variable name is required", ErrValidation)
	}
	if len(key) > 128 {
		return fmt.Errorf("%w: environment variable name is too long", ErrValidation)
	}
	if strings.ContainsAny(key, "= \t\r\n\x00") {
		return fmt.Errorf("%w: environment variable name %q must not contain spaces, '=' or NUL", ErrValidation, key)
	}
	return nil
}

// normalizeStorages trims and validates the volume map: named rows with a
// container path and either a managed host path, a host bind confined to
// <managed root>/<appID>, or a Docker named volume. An empty host path is
// derived to a managed directory, so a client never needs to know the
// application id up front. The rules mirror volumeSpecs, so a bad mapping is a
// 400 at write time rather than a failed deployment later.
func normalizeStorages(appID uuid.UUID, storages []Storage) ([]Storage, error) {
	root := managedVolumeRoot()
	normalized := make([]Storage, 0, len(storages))
	seen := make(map[string]bool, len(storages))
	seenDirs := make(map[string]string, len(storages))
	seenNamed := make(map[string]string, len(storages))
	for _, storage := range storages {
		row := Storage{
			Name:          strings.TrimSpace(storage.Name),
			HostPath:      strings.TrimSpace(storage.HostPath),
			ContainerPath: strings.TrimSpace(storage.ContainerPath),
		}
		if row.Name == "" {
			return nil, fmt.Errorf("%w: storage name is required", ErrValidation)
		}
		if seen[row.Name] {
			return nil, fmt.Errorf("%w: duplicate storage name %q", ErrValidation, row.Name)
		}
		seen[row.Name] = true
		// Two distinct names that sanitize to the same directory would share
		// one host path once derived; reject rather than silently fuse them.
		dir := storageDirName(row.Name)
		if other, ok := seenDirs[dir]; ok {
			return nil, fmt.Errorf("%w: storage names %q and %q map to the same managed directory %q",
				ErrValidation, other, row.Name, dir)
		}
		seenDirs[dir] = row.Name
		host, err := managedHostPath(root, appID, row.Name, row.HostPath)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(host, "/") {
			// Named volumes are namespaced with the storage name; reject two
			// rows whose (sanitized) names collapse onto one volume.
			if other, ok := seenNamed[host]; ok {
				return nil, fmt.Errorf("%w: storage names %q and %q map to the same named volume %q",
					ErrValidation, other, row.Name, host)
			}
			seenNamed[host] = row.Name
		}
		row.HostPath = host
		normalized = append(normalized, row)
	}
	if _, err := volumeSpecs(root, appID, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// envEntries merges the stored plain env vars and sealed secrets into the API
// view of the environment, sorted by key. Secrets appear as their reference —
// the ciphertext and the plaintext stay on the server.
func (s *Service) envEntries(ctx context.Context, appID uuid.UUID) ([]EnvEntry, error) {
	envVars, secrets, err := s.repo.ListEnvConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	entries := make([]EnvEntry, 0, len(envVars)+len(secrets))
	for _, variable := range envVars {
		entries = append(entries, EnvEntry{Key: variable.Key, Value: variable.Value})
	}
	for _, secret := range secrets {
		entries = append(entries, EnvEntry{Key: secret.Key, Value: secretRefPrefix + secret.ID.String()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}
