package domainwatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type serviceFixture struct {
	store     *store.Store
	serviceID uuid.UUID
	domainID  uuid.UUID
	projectID uuid.UUID
	now       time.Time
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	st := integrationStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, "domain-watch-svc-"+time.Now().Format("150405.000000000")+"@example.com", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: "domain-watch-svc-" + time.Now().Format("150405.000000000"),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: "domain-watch-svc-node-" + time.Now().Format("150405.000000000"), Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	serviceRow, err := st.CreateService(ctx, sqlc.CreateServiceParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        user.ID,
		ServerID:      serverRow.ID,
		EnvironmentID: environment.ID,
		Name:          "domain-watch-service",
		Status:        "running",
		ComposeYaml:   "services:\n  web:\n    image: nginx:alpine\n",
		Env:           []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	domainID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO service_domains
		(id, service_id, compose_service, port, domain, protocol, disabled, is_primary, created_at, updated_at)
		VALUES ($1, $2, 'web', 80, $3, 'https', false, true, $4, $4)`,
		domainID, serviceRow.ID, "svc-"+domainID.String()[:8]+".example.com", now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert service domain: %v", err)
	}
	return serviceFixture{
		store:     st,
		serviceID: uuid.UUID(serviceRow.ID.Bytes),
		domainID:  domainID,
		projectID: uuid.UUID(project.ID.Bytes),
		now:       now,
	}
}

func (f serviceFixture) target(t *testing.T) Target {
	t.Helper()
	target, err := (&Service{st: f.store}).target(context.Background(), OwnerService, f.serviceID, f.domainID)
	if err != nil {
		t.Fatalf("service target: %v", err)
	}
	return target
}

func countMonitorRows(t *testing.T, st *store.Store, domainID uuid.UUID) (settings, state, results int) {
	t.Helper()
	ctx := context.Background()
	for _, query := range []struct {
		sql  string
		scan *int
	}{
		{"SELECT count(*) FROM domain_monitor_settings WHERE domain_id = $1", &settings},
		{"SELECT count(*) FROM domain_monitor_state WHERE domain_id = $1", &state},
		{"SELECT count(*) FROM domain_probe_results WHERE domain_id = $1", &results},
	} {
		if err := st.DB.QueryRow(ctx, query.sql, domainID).Scan(query.scan); err != nil {
			t.Fatalf("count monitor rows: %v", err)
		}
	}
	return settings, state, results
}

func seedDomainIncident(t *testing.T, st *store.Store, identity string, domainID, projectID uuid.UUID) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := st.DB.Exec(context.Background(), `INSERT INTO incidents
		(id, identity_key, detector_key, scope_kind, scope_id, project_id,
		 resource_type, resource_id, resource_name, severity, lifecycle, coverage,
		 first_observed_at, last_observed_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'project', $4, $4, 'domain', $5, 'service / host',
		 'critical', 'firing', 'fresh', $6, $6, $6, $6)`,
		uuid.New(), identity, DetectorReachability, projectID, domainID, now); err != nil {
		t.Fatalf("seed domain incident: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM incidents WHERE identity_key = $1", identity)
	})
}

func TestServiceDomainTargetProbeAndSettings(t *testing.T) {
	fixture := newServiceFixture(t)
	ctx := context.Background()
	now := fixture.now
	runner := &countingRunner{now: func() time.Time { return now }}
	reporter := &recordingReporter{}
	svc := NewService(Config{
		Store:     fixture.store,
		Incidents: reporter,
		Now:       func() time.Time { return now },
		Prober:    runner,
		Grace:     ConfigGrace,
	})
	svc.startedAt = now.Add(-time.Hour)

	target := fixture.target(t)
	if target.OwnerKind != OwnerService || target.OwnerID != fixture.serviceID {
		t.Fatalf("service target owner = %q %s", target.OwnerKind, target.OwnerID)
	}
	if target.OwnerName != "domain-watch-service" || !strings.HasPrefix(target.Domain, "svc-") {
		t.Fatalf("service target = %+v", target)
	}
	if rawURL, err := target.URL(); err != nil || !strings.HasPrefix(rawURL, "https://svc-") {
		t.Fatalf("target URL = %q %v", rawURL, err)
	}

	// A service without a deploy is not probed; the gap is unknown, not failed.
	svc.tick(ctx)
	if _, _, results := countMonitorRows(t, fixture.store, fixture.domainID); results != 0 {
		t.Fatalf("never-deployed service was probed: %d results", results)
	}
	if !containsString(reporter.closedReasons, "not_deployed") {
		t.Fatalf("never-deployed close reasons = %v", reporter.closedReasons)
	}

	// A running deploy makes the target probeable; the result names the
	// service owner.
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO service_deploys
		(service_id, state, compose_yaml, created_at, finished_at)
		VALUES ($1, 'running', 'services: {}', $2, $3)`,
		fixture.serviceID, now.Add(-5*time.Minute), now.Add(-4*time.Minute)); err != nil {
		t.Fatalf("insert service deploy: %v", err)
	}
	now = now.Add(time.Second)
	svc.tick(ctx)
	if _, _, results := countMonitorRows(t, fixture.store, fixture.domainID); results != 1 {
		t.Fatalf("running service was not probed: %d results", results)
	}
	var appID pgtype.UUID
	var serviceID pgtype.UUID
	if err := fixture.store.DB.QueryRow(ctx, `SELECT application_id, service_id
		FROM domain_probe_results WHERE domain_id = $1`, fixture.domainID).Scan(&appID, &serviceID); err != nil {
		t.Fatalf("read service result owner: %v", err)
	}
	if appID.Valid || !serviceID.Valid || uuid.UUID(serviceID.Bytes) != fixture.serviceID {
		t.Fatalf("result owner = app %v service %v", appID, serviceID)
	}

	view, err := svc.Get(ctx, OwnerService, fixture.serviceID, fixture.domainID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.ServiceID != fixture.serviceID.String() || view.ApplicationID != "" {
		t.Fatalf("service view owner = %q %q", view.ApplicationID, view.ServiceID)
	}
	view, err = svc.SaveSettings(ctx, OwnerService, fixture.serviceID, fixture.domainID, SettingsInput{
		Enabled: true, Path: "/health", ExpectedMin: 200, ExpectedMax: 399, Timeout: 5,
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if view.Path != "/health" || view.ServiceID != fixture.serviceID.String() {
		t.Fatalf("saved service view = %+v", view)
	}

	// Another service id cannot read this domain.
	if _, err := svc.Get(ctx, OwnerService, uuid.New(), fixture.domainID); err == nil {
		t.Fatal("foreign owner read the service domain")
	}

	// Disabled rows and rows of a soft-deleted service are not targets.
	var userID, serverID, environmentID pgtype.UUID
	if err := fixture.store.DB.QueryRow(ctx, `SELECT user_id, server_id, environment_id
		FROM services WHERE id = $1`, fixture.serviceID).Scan(&userID, &serverID, &environmentID); err != nil {
		t.Fatalf("read service owner columns: %v", err)
	}
	disabledID := uuid.New()
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO service_domains
		(id, service_id, compose_service, port, domain, protocol, disabled, is_primary, created_at, updated_at)
		VALUES ($1, $2, 'web', 80, $3, 'https', true, false, $4, $4)`,
		disabledID, fixture.serviceID, "svc-disabled-"+disabledID.String()[:8]+".example.com", now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert disabled service domain: %v", err)
	}
	deletedServiceID := uuid.New()
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO services
		(id, user_id, server_id, environment_id, name, status, compose_yaml, env, deleted_at)
		VALUES ($1, $2, $3, $4, $5, 'running', 'services: {}', '{}'::jsonb, now())`,
		deletedServiceID, userID, serverID, environmentID, "domain-watch-deleted"); err != nil {
		t.Fatalf("insert soft-deleted service: %v", err)
	}
	deletedDomainID := uuid.New()
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO service_domains
		(id, service_id, compose_service, port, domain, protocol, disabled, is_primary, created_at, updated_at)
		VALUES ($1, $2, 'web', 80, $3, 'https', false, true, $4, $4)`,
		deletedDomainID, deletedServiceID, "svc-deleted-"+deletedDomainID.String()[:8]+".example.com", now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert soft-deleted service domain: %v", err)
	}
	targets, err := svc.targets(ctx)
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	byDomain := make(map[uuid.UUID]Target, len(targets))
	for _, item := range targets {
		byDomain[item.DomainID] = item
	}
	if _, ok := byDomain[disabledID]; ok {
		t.Fatal("disabled service domain was a probe target")
	}
	if _, ok := byDomain[deletedDomainID]; ok {
		t.Fatal("soft-deleted service domain was a probe target")
	}
	if _, ok := byDomain[fixture.domainID]; !ok {
		t.Fatal("enabled service domain disappeared from probe targets")
	}

	// A stopped service stays a target: its incidents close as intentionally
	// stopped through the shared suppression path.
	if _, err := fixture.store.DB.Exec(ctx, `UPDATE services SET status = 'stopped' WHERE id = $1`,
		fixture.serviceID); err != nil {
		t.Fatalf("stop service: %v", err)
	}
	targets, err = svc.targets(ctx)
	if err != nil {
		t.Fatalf("stopped targets: %v", err)
	}
	stopped := Target{}
	foundStopped := false
	for _, item := range targets {
		if item.DomainID == fixture.domainID {
			stopped = item
			foundStopped = true
			break
		}
	}
	if !foundStopped {
		t.Fatal("stopped service domain disappeared from probe targets")
	}
	if stopped.DesiredState != "stopped" || suppressionReason(stopped) != "intentionally_stopped" {
		t.Fatalf("stopped service target = %q / %q", stopped.DesiredState, suppressionReason(stopped))
	}
}

func TestServiceDomainDeleteClosesIncidentAndCleansHistory(t *testing.T) {
	fixture := newServiceFixture(t)
	ctx := context.Background()
	now := fixture.now
	reporter := &recordingReporter{}
	svc := NewService(Config{
		Store:     fixture.store,
		Incidents: reporter,
		Now:       func() time.Time { return now },
		Prober:    &countingRunner{now: func() time.Time { return now }},
		Grace:     ConfigGrace,
	})
	svc.startedAt = now.Add(-time.Hour)

	target := fixture.target(t)
	if _, err := svc.SaveSettings(ctx, OwnerService, fixture.serviceID, fixture.domainID, SettingsInput{
		Enabled: true, Path: "/", ExpectedMin: 200, ExpectedMax: 399, Timeout: 5,
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if err := svc.record(ctx, target, Result{Outcome: OutcomeHealthy, StatusCode: 200, At: now}); err != nil {
		t.Fatalf("record: %v", err)
	}
	seedDomainIncident(t, fixture.store, target.incidentIdentity(), fixture.domainID, fixture.projectID)

	if _, err := fixture.store.DB.Exec(ctx, `DELETE FROM service_domains WHERE id = $1`, fixture.domainID); err != nil {
		t.Fatalf("remove service domain: %v", err)
	}
	if settings, state, results := countMonitorRows(t, fixture.store, fixture.domainID); settings+state+results != 0 {
		t.Fatalf("removed domain kept monitor rows: settings=%d state=%d results=%d", settings, state, results)
	}
	svc.tick(ctx)
	if !containsString(reporter.closed, target.incidentIdentity()) {
		t.Fatalf("removed service domain incident not closed: %v", reporter.closed)
	}
	for i, identity := range reporter.closed {
		if identity == target.incidentIdentity() && reporter.closedReasons[i] != "domain_removed" {
			t.Fatalf("removed service domain close reason = %q", reporter.closedReasons[i])
		}
	}

	// Deleting the service cascades its domains and their monitor rows.
	secondID := uuid.New()
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO service_domains
		(id, service_id, compose_service, port, domain, protocol, disabled, is_primary, created_at, updated_at)
		VALUES ($1, $2, 'web', 80, $3, 'https', false, true, $4, $4)`,
		secondID, fixture.serviceID, "svc-second-"+secondID.String()[:8]+".example.com", now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert second service domain: %v", err)
	}
	second, err := (&Service{st: fixture.store}).target(ctx, OwnerService, fixture.serviceID, secondID)
	if err != nil {
		t.Fatalf("second target: %v", err)
	}
	if _, err := svc.SaveSettings(ctx, OwnerService, fixture.serviceID, secondID, SettingsInput{
		Enabled: true, Path: "/", ExpectedMin: 200, ExpectedMax: 399, Timeout: 5,
	}); err != nil {
		t.Fatalf("second SaveSettings: %v", err)
	}
	if err := svc.record(ctx, second, Result{Outcome: OutcomeHealthy, StatusCode: 200, At: now}); err != nil {
		t.Fatalf("second record: %v", err)
	}
	seedDomainIncident(t, fixture.store, second.incidentIdentity(), secondID, fixture.projectID)
	if _, err := fixture.store.DB.Exec(ctx, `DELETE FROM services WHERE id = $1`, fixture.serviceID); err != nil {
		t.Fatalf("delete service: %v", err)
	}
	if settings, state, results := countMonitorRows(t, fixture.store, secondID); settings+state+results != 0 {
		t.Fatalf("deleted service kept monitor rows: settings=%d state=%d results=%d", settings, state, results)
	}
	svc.tick(ctx)
	if !containsString(reporter.closed, second.incidentIdentity()) {
		t.Fatalf("deleted service domain incident not closed: %v", reporter.closed)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestServiceWebsiteCheckHandler(t *testing.T) {
	fixture := newServiceFixture(t)
	runner := &countingRunner{now: time.Now}
	svc := NewService(Config{
		Store:     fixture.store,
		Incidents: &recordingReporter{},
		Prober:    runner,
	})
	router := chi.NewRouter()
	Mount(router, func(next http.Handler) http.Handler { return next }, svc)

	base := "/v1/services/" + fixture.serviceID.String() + "/domains/" + fixture.domainID.String() + "/monitoring"

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/services/"+fixture.serviceID.String()+"/domains/"+uuid.New().String()+"/monitoring", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown domain GET = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, base,
		strings.NewReader(`{"enabled":true,"path":"/health","expected_min":200,"expected_max":399,"timeout_seconds":5}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"service_id":"`+fixture.serviceID.String()+`"`) ||
		strings.Contains(rec.Body.String(), `"application_id"`) {
		t.Fatalf("service view body = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		WebsiteCheck View `json:"website_check"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if envelope.WebsiteCheck.Path != "/health" || envelope.WebsiteCheck.ServiceID != fixture.serviceID.String() {
		t.Fatalf("GET view = %+v", envelope.WebsiteCheck)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, base+"/test", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"outcome":"healthy"`) {
		t.Fatalf("POST test = %d %s", rec.Code, rec.Body.String())
	}

	// The application route keeps the application owner field.
	appFixture := newIntegrationFixture(t)
	appRouter := chi.NewRouter()
	appService := NewService(Config{
		Store:     appFixture.store,
		Incidents: &recordingReporter{},
		Prober:    runner,
	})
	Mount(appRouter, func(next http.Handler) http.Handler { return next }, appService)
	rec = httptest.NewRecorder()
	appRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/v1/applications/"+appFixture.appID.String()+"/domains/"+appFixture.domainID.String()+"/monitoring", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("application GET = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"application_id":"`+appFixture.appID.String()+`"`) ||
		strings.Contains(rec.Body.String(), `"service_id"`) {
		t.Fatalf("application view body = %s", rec.Body.String())
	}
}
