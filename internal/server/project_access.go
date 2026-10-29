package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

const grantRecheckPeriod = time.Second

func (s *Server) canAccessResource(ctx context.Context, userID uuid.UUID, kind, rawID string) bool {
	if s.persistence == nil || s.persistence.DB == nil {
		return false
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return false
	}
	project, err := s.persistence.ResourceProject(ctx, kind, id)
	if err != nil {
		return false
	}
	allowed, err := s.persistence.HasProjectGrant(ctx, userID, project)
	return err == nil && allowed
}

func projectRouteKey(r *http.Request) string {
	return r.Method + " " + chi.RouteContext(r.Context()).RoutePattern()
}

func hideResource(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
}

// projectAccess is shared by every resource mount, including API-token requests.
// A missing policy or unresolved relationship always denies member access.
func (s *Server) projectAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isOwnerAccount(r) {
			next.ServeHTTP(w, r)
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || s.auth == nil {
			hideResource(w)
			return
		}
		user, err := s.auth.Me(r.Context(), userID)
		if err != nil || user.Role != auth.RoleMember {
			hideResource(w)
			return
		}
		policy, ok := projectPolicies[projectRouteKey(r)]
		if !ok {
			hideResource(w)
			return
		}
		if policy.ownerOnly {
			hideResource(w)
			return
		}
		if policy.listKey == "" && !policy.create && !s.canAccessResource(r.Context(), userID, policy.kind, chi.URLParam(r, "id")) {
			hideResource(w)
			return
		}
		if !s.authorizeProjectReferences(r, userID, policy) {
			hideResource(w)
			return
		}

		// Service logs are the only streaming resource response. Cancel idle reads
		// on revocation as well as checking before each delivered chunk.
		if policy.kind == "service" && strings.HasSuffix(r.URL.Path, "/logs") {
			s.serveProjectStream(w, r, next, userID, policy)
			return
		}
		responseStarted := time.Now()
		response := &projectResponseBuffer{header: make(http.Header)}
		next.ServeHTTP(response, r)
		for key, values := range response.header {
			if !strings.EqualFold(key, "Content-Length") {
				w.Header()[key] = append([]string(nil), values...)
			}
		}
		w.Header().Del("Content-Length")
		if response.status >= 400 {
			if policy.kind == "application" && strings.Contains(projectRouteKey(r), "/domains") &&
				(response.status == http.StatusBadRequest || response.status == http.StatusConflict) {
				// Private applications, redirects and panel reservations share one error class.
				timer := time.NewTimer(max(0, 50*time.Millisecond-time.Since(responseStarted)))
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-r.Context().Done():
					return
				}
				writeJSON(w, http.StatusConflict, apiError{Message: "domain unavailable"})
				return
			}
			writeJSON(w, response.status, apiError{Message: http.StatusText(response.status)})
			return
		}
		if response.body.Len() == 0 {
			w.WriteHeader(response.statusCode())
			return
		}
		var payload any
		decoder := json.NewDecoder(&response.body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			hideResource(w)
			return
		}
		if policy.listKey != "" {
			envelope, ok := payload.(map[string]any)
			if !ok {
				hideResource(w)
				return
			}
			rows, ok := envelope[policy.listKey].([]any)
			if !ok && envelope[policy.listKey] != nil {
				hideResource(w)
				return
			}

			ids := make([]uuid.UUID, 0, len(rows))
			for _, row := range rows {
				item, ok := row.(map[string]any)
				if !ok {
					continue
				}
				raw, _ := item["id"].(string)
				if id, err := uuid.Parse(raw); err == nil {
					ids = append(ids, id)
				}
			}
			if s.persistence == nil || s.persistence.DB == nil {
				hideResource(w)
				return
			}
			allowed, err := s.persistence.GrantedResourceIDs(r.Context(), userID, policy.kind, ids)
			if err != nil {
				hideResource(w)
				return
			}
			filtered := make([]any, 0, len(rows))
			for _, row := range rows {
				item, ok := row.(map[string]any)
				if !ok {
					continue
				}
				raw, _ := item["id"].(string)
				id, err := uuid.Parse(raw)
				if err == nil && allowed[id] {
					filtered = append(filtered, row)
				}
			}
			// Return only the filtered collection; counts and pagination from the
			// unfiltered query would disclose resources outside the grant.
			payload = map[string]any{policy.listKey: filtered}
		}
		redactOperatorResources(payload)
		writeJSON(w, response.statusCode(), payload)
	})
}

// authorizeProjectReferences checks all IDs a handler could use in addition to
// the path target, including moves and restore/rollback references.
func (s *Server) authorizeProjectReferences(r *http.Request, userID uuid.UUID, policy projectPolicy) bool {
	refs := map[string]string{"project_id": "project", "environment_id": "environment", "application_id": "application", "database_id": "database", "service_id": "service", "deployment_id": "deployment", "backup_id": "backup", "schedule_id": "schedule"}
	for param, kind := range map[string]string{"deployment_id": "deployment", "backupId": "backup", "scheduleId": "schedule"} {
		if id := chi.URLParam(r, param); id != "" && !s.canAccessResource(r.Context(), userID, kind, id) {
			return false
		}
	}
	// A resource filter must resolve to a grant, otherwise a member could tell
	// an existing resource in another project from a missing one.
	queryType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	queryID := strings.TrimSpace(r.URL.Query().Get("resource_id"))
	if queryType != "" || queryID != "" {
		if queryType == "" || queryID == "" || !s.canAccessResource(r.Context(), userID, queryType, queryID) {
			return false
		}
	}
	for key, values := range r.URL.Query() {
		if kind, ok := refs[strings.ToLower(key)]; ok {
			for _, id := range values {
				if !s.canAccessResource(r.Context(), userID, kind, id) {
					return false
				}
			}
		}
		if operatorReference(key) {
			return false
		}
	}
	if r.Body == nil || r.Body == http.NoBody {
		return !policy.create
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAuthBodyBytes+1))
	if err != nil || len(data) > maxAuthBodyBytes {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	if len(bytes.TrimSpace(data)) == 0 {
		return !policy.create
	}
	if err := validateProjectJSONKeys(data); err != nil {
		return false
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		return false
	}
	normalized := make(map[string]json.RawMessage, len(body))
	for key, value := range body {
		key = projectReferenceKey(key)
		if key == "" {
			continue
		}
		normalized[key] = value
		if kind, ok := refs[key]; ok {
			var id string
			if json.Unmarshal(value, &id) != nil || !s.canAccessResource(r.Context(), userID, kind, id) {
				return false
			}
		}
		if operatorReference(key) && string(value) != "null" && string(value) != `""` {
			return false
		}
	}
	if policy.create && (policy.kind == "application" || policy.kind == "database" || policy.kind == "service") {
		var environment string
		if json.Unmarshal(normalized["environment_id"], &environment) != nil || !s.canAccessResource(r.Context(), userID, "environment", environment) {
			return false
		}
		// Placement is a platform concern; members never enumerate nodes.
		var serverID uuid.UUID
		if s.persistence.DB.QueryRow(r.Context(), `SELECT id FROM servers WHERE status='ready' ORDER BY created_at,id LIMIT 1`).Scan(&serverID) != nil {
			return false
		}
		for key := range body {
			if projectReferenceKey(key) == "server_id" {
				delete(body, key)
			}
		}
		body["server_id"], _ = json.Marshal(serverID.String())
		encoded, err := json.Marshal(body)
		if err != nil {
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader(encoded))
	}
	resourceType, hasType := normalized["resource_type"]
	resourceID, hasID := normalized["resource_id"]
	if hasType || hasID || policy.create && policy.kind == "channel" {
		var kind, id string
		if json.Unmarshal(resourceType, &kind) != nil || json.Unmarshal(resourceID, &id) != nil {
			return false
		}
		if kind != "application" && kind != "database" && kind != "service" {
			return false
		}
		if !s.canAccessResource(r.Context(), userID, kind, id) {
			return false
		}
	}
	return true
}

func operatorReference(key string) bool {
	switch strings.ToLower(key) {
	case "server_id", "target_id", "provider_id", "private_key_id", "ssh_key_id", "github_app_id", "provider_connection_id":
		return true
	}
	return false
}

func redactOperatorResources(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if key == "error" && item != "" {
				value[key] = "operation failed"
				continue
			}
			if operatorReference(key) || key == "server_name" || key == "file_manager" || key == "has_registry_credential" || key == "location" {
				delete(value, key)
				continue
			}
			switch key {
			case "env", "build_args", "config":
				continue
			}
			redactOperatorResources(item)
		}
	case []any:
		for _, item := range value {
			redactOperatorResources(item)
		}
	}
}

type projectResponseBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *projectResponseBuffer) Header() http.Header { return b.header }
func (b *projectResponseBuffer) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *projectResponseBuffer) Write(data []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(data)
}
func (b *projectResponseBuffer) statusCode() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

func (s *Server) serveProjectStream(w http.ResponseWriter, r *http.Request, next http.Handler, userID uuid.UUID, policy projectPolicy) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	allowed := func() bool { return s.canAccessResource(ctx, userID, policy.kind, chi.URLParam(r, "id")) }
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(grantRecheckPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !allowed() {
					cancel()
					return
				}
			}
		}
	}()
	next.ServeHTTP(&projectStreamWriter{ResponseWriter: w, allowed: allowed}, r.WithContext(ctx))
}

type projectStreamWriter struct {
	http.ResponseWriter
	allowed func() bool
}

func (w *projectStreamWriter) Write(data []byte) (int, error) {
	if !w.allowed() {
		return 0, errors.New("project access revoked")
	}
	return w.ResponseWriter.Write(data)
}
func (w *projectStreamWriter) Flush() {
	if w.allowed() {
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}
