package files

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newTransferRouter(svc FileService, authorize TransferAuthorizer) http.Handler {
	r := chi.NewRouter()
	Mount(r, passthroughAuth, authorize, svc, nil)
	return r
}

func TestPlanTransferRoute(t *testing.T) {
	id := uuid.New()
	destination := uuid.New()
	plan := TransferPlan{Mode: TransferModeDirect, Conflicts: []string{}, Sources: []TransferPlanSource{{Name: "a.txt", Kind: "file", Size: 3}}}
	svc := &fakeService{transferPlan: plan}
	var seenSource, seenDestination uuid.UUID
	authorize := TransferAuthorizerFunc(func(_ context.Context, sourceID, destinationID uuid.UUID) error {
		seenSource, seenDestination = sourceID, destinationID
		return nil
	})
	h := newTransferRouter(svc, authorize)

	body := `{"source_root":"volumes","source_directory":"data","sources":["a.txt"],"destination":{"server_id":"` + destination.String() + `","root":"volumes","path":"incoming"}}`
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/transfers/plan", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if seenSource != id || seenDestination != destination {
		t.Fatalf("authorized %s -> %s", seenSource, seenDestination)
	}
	var decoded TransferPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if decoded.Mode != TransferModeDirect || len(decoded.Sources) != 1 {
		t.Fatalf("plan = %+v", decoded)
	}
	if svc.seenTransfer == nil || svc.seenTransfer.Destination.ServerID != destination {
		t.Fatalf("service saw %+v", svc.seenTransfer)
	}
}

func TestPlanTransferRequiresBothNodeAuthorization(t *testing.T) {
	id := uuid.New()
	destination := uuid.New()
	body := `{"source_root":"volumes","sources":["a.txt"],"destination":{"server_id":"` + destination.String() + `","root":"volumes","path":""}}`

	for label, authorize := range map[string]TransferAuthorizer{
		"missing authorizer": nil,
		"denied": TransferAuthorizerFunc(func(context.Context, uuid.UUID, uuid.UUID) error {
			return ErrForbidden
		}),
	} {
		t.Run(label, func(t *testing.T) {
			svc := &fakeService{}
			rec := doWrite(t, newTransferRouter(svc, authorize), http.MethodPost, "/v1/servers/"+id.String()+"/files/transfers/plan", body, nil)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
			if svc.called("plan_transfer") || svc.called("transfer") {
				t.Fatal("the service ran before both-node authorization passed")
			}
		})
	}
}

func TestExecuteTransferStreamsNDJSON(t *testing.T) {
	id := uuid.New()
	destination := uuid.New()
	summary := TransferSummary{TransferID: "t", Mode: TransferModeDirect, Status: TransferStatusCopied, Copied: 1}
	svc := &fakeService{transferEvents: []TransferEvent{
		{Type: TransferEventPhase, Phase: TransferPhaseTransferring},
		{Type: TransferEventResult, Name: "a.txt", Status: TransferStatusCopied},
		{Type: TransferEventSummary, Summary: &summary, Status: summary.Status},
	}}
	authorize := TransferAuthorizerFunc(func(context.Context, uuid.UUID, uuid.UUID) error { return nil })
	h := newTransferRouter(svc, authorize)

	body := `{"source_root":"volumes","sources":["a.txt"],"destination":{"server_id":"` + destination.String() + `","root":"volumes","path":""}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/servers/"+id.String()+"/files/transfers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/x-ndjson" {
		t.Fatalf("content-type = %q", contentType)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (%s)", len(lines), rec.Body.String())
	}
	var summaryLine TransferEvent
	if err := json.Unmarshal([]byte(lines[2]), &summaryLine); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summaryLine.Type != TransferEventSummary || summaryLine.Summary == nil || summaryLine.Summary.Copied != 1 {
		t.Fatalf("summary event = %+v", summaryLine)
	}
}

func TestExecuteTransferPreStreamErrorIsJSON(t *testing.T) {
	id := uuid.New()
	destination := uuid.New()
	svc := &fakeService{transferErr: ErrConflict}
	authorize := TransferAuthorizerFunc(func(context.Context, uuid.UUID, uuid.UUID) error { return nil })
	body := `{"source_root":"volumes","sources":["a.txt"],"destination":{"server_id":"` + destination.String() + `","root":"volumes","path":""}}`
	rec := doWrite(t, newTransferRouter(svc, authorize), http.MethodPost, "/v1/servers/"+id.String()+"/files/transfers", body, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content-type = %q, want JSON for a pre-stream failure", contentType)
	}
}

func TestTransferRoutesRejectInvalidBodies(t *testing.T) {
	id := uuid.New()
	authorize := TransferAuthorizerFunc(func(context.Context, uuid.UUID, uuid.UUID) error { return nil })
	for _, path := range []string{"/files/transfers/plan", "/files/transfers"} {
		rec := doWrite(t, newTransferRouter(&fakeService{}, authorize), http.MethodPost, "/v1/servers/"+id.String()+path, "{", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, rec.Code)
		}
	}
}
