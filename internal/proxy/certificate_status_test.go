package proxy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeStatusStore is an in-memory CertificateStatusStore.
type fakeStatusStore struct {
	targets []CertificateStatusTarget
	err     error
}

func (s *fakeStatusStore) ListCertificateStatusTargets(context.Context) ([]CertificateStatusTarget, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.targets, nil
}

// fakeACMEReader answers one canned response (or error).
type fakeACMEReader struct {
	response *agentv1.ReadACMEStorageResponse
	err      error
	closed   *int
}

func (r *fakeACMEReader) ReadACMEStorage(context.Context, *agentv1.ReadACMEStorageRequest, ...grpc.CallOption) (*agentv1.ReadACMEStorageResponse, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.response, nil
}

func (r *fakeACMEReader) Close() error {
	if r.closed != nil {
		*r.closed++
	}
	return nil
}

// statusCertificate builds a certificate intent fixture.
func statusCertificate(domain string) DomainCertificate {
	return DomainCertificate{ID: uuid.New(), ApplicationID: uuid.New(), Domain: domain, Challenge: ChallengeHTTP01}
}

// storedCertificate builds one agent metadata entry.
func storedCertificate(resolver, main string, sans []string, notAfter time.Time) *agentv1.ACMECertificateInfo {
	return &agentv1.ACMECertificateInfo{
		Resolver: resolver,
		Main:     main,
		Sans:     sans,
		NotAfter: timestamppb.New(notAfter),
	}
}

func TestCertificateStatusesMapsPresentAbsentUnknown(t *testing.T) {
	serverStored := uuid.New()
	serverEmpty := uuid.New()
	serverDown := uuid.New()
	serverError := uuid.New()

	expiry := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	certificateMain := statusCertificate("app.example.com")
	certificateSAN := statusCertificate("shop.example.com")
	certificateAbsent := statusCertificate("absent.example.com")
	certificateNoStorage := statusCertificate("fresh.example.com")
	certificateUnreachable := statusCertificate("down.example.com")
	certificateRPCError := statusCertificate("error.example.com")
	certificateUnassigned := statusCertificate("unassigned.example.com")

	store := &fakeStatusStore{targets: []CertificateStatusTarget{
		{CertificateID: certificateMain.ID, Domain: certificateMain.Domain, ServerID: serverStored},
		{CertificateID: certificateSAN.ID, Domain: certificateSAN.Domain, ServerID: serverStored},
		{CertificateID: certificateAbsent.ID, Domain: certificateAbsent.Domain, ServerID: serverStored},
		{CertificateID: certificateNoStorage.ID, Domain: certificateNoStorage.Domain, ServerID: serverEmpty},
		{CertificateID: certificateUnreachable.ID, Domain: certificateUnreachable.Domain, ServerID: serverDown},
		{CertificateID: certificateRPCError.ID, Domain: certificateRPCError.Domain, ServerID: serverError},
		{CertificateID: certificateUnassigned.ID, Domain: certificateUnassigned.Domain, ServerID: uuid.Nil},
	}}

	dials := map[uuid.UUID]int{}
	closed := 0
	dial := func(_ context.Context, serverID uuid.UUID) (ACMEReader, error) {
		dials[serverID]++
		switch serverID {
		case serverStored:
			return &fakeACMEReader{
				response: &agentv1.ReadACMEStorageResponse{
					Present: true,
					Certificates: []*agentv1.ACMECertificateInfo{
						storedCertificate("letsencrypt", "app.example.com", nil, expiry),
						storedCertificate("letsencrypt", "other.example.com", []string{"shop.example.com"}, expiry.Add(time.Hour)),
					},
				},
				closed: &closed,
			}, nil
		case serverEmpty:
			return &fakeACMEReader{response: &agentv1.ReadACMEStorageResponse{}, closed: &closed}, nil
		case serverDown:
			return nil, errors.New("dial: connection refused")
		case serverError:
			return &fakeACMEReader{err: errors.New("rpc: internal"), closed: &closed}, nil
		default:
			t.Fatalf("unexpected dial to %s", serverID)
			return nil, nil
		}
	}
	svc := NewDefaultCertificateStatusService(CertificateStatusConfig{Store: store, Dial: dial, Logger: discardLogger()})
	if svc == nil {
		t.Fatal("service is nil")
	}

	certificates := []DomainCertificate{
		certificateMain, certificateSAN, certificateAbsent, certificateNoStorage,
		certificateUnreachable, certificateRPCError, certificateUnassigned,
	}
	statuses := svc.CertificateStatuses(context.Background(), certificates)
	if len(statuses) != len(certificates) {
		t.Fatalf("statuses = %d, want one per certificate (%d)", len(statuses), len(certificates))
	}

	want := map[uuid.UUID]struct {
		status   CertificateStatus
		notAfter time.Time
	}{
		certificateMain.ID:        {CertificateStatusPresent, expiry},
		certificateSAN.ID:         {CertificateStatusPresent, expiry.Add(time.Hour)},
		certificateAbsent.ID:      {CertificateStatusAbsent, time.Time{}},
		certificateNoStorage.ID:   {CertificateStatusAbsent, time.Time{}},
		certificateUnreachable.ID: {CertificateStatusUnknown, time.Time{}},
		certificateRPCError.ID:    {CertificateStatusUnknown, time.Time{}},
		certificateUnassigned.ID:  {CertificateStatusUnknown, time.Time{}},
	}
	for id, expected := range want {
		got := statuses[id]
		if got.Status != expected.status {
			t.Errorf("status(%s) = %q, want %q", id, got.Status, expected.status)
		}
		if !got.NotAfter.Equal(expected.notAfter) {
			t.Errorf("notAfter(%s) = %s, want %s", id, got.NotAfter, expected.notAfter)
		}
	}

	// One dial and one close per reachable node, shared by its certificates;
	// the unreachable node is dialed once too.
	if dials[serverStored] != 1 || dials[serverEmpty] != 1 || dials[serverDown] != 1 || dials[serverError] != 1 {
		t.Fatalf("dials = %v, want exactly one per node", dials)
	}
	if closed != 3 {
		t.Fatalf("closed = %d, want 3 (the two successful readings and the RPC error)", closed)
	}
}

func TestCertificateStatusesWildcardIsOneLabel(t *testing.T) {
	server := uuid.New()
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	oneLabel := statusCertificate("a.example.com")
	twoLabels := statusCertificate("a.b.example.com")
	store := &fakeStatusStore{targets: []CertificateStatusTarget{
		{CertificateID: oneLabel.ID, Domain: oneLabel.Domain, ServerID: server},
		{CertificateID: twoLabels.ID, Domain: twoLabels.Domain, ServerID: server},
	}}
	dial := func(context.Context, uuid.UUID) (ACMEReader, error) {
		return &fakeACMEReader{response: &agentv1.ReadACMEStorageResponse{
			Present:      true,
			Certificates: []*agentv1.ACMECertificateInfo{storedCertificate("letsencrypt", "other.example.com", []string{"*.example.com"}, expiry)},
		}}, nil
	}
	svc := NewDefaultCertificateStatusService(CertificateStatusConfig{Store: store, Dial: dial, Logger: discardLogger()})
	statuses := svc.CertificateStatuses(context.Background(), []DomainCertificate{oneLabel, twoLabels})
	if statuses[oneLabel.ID].Status != CertificateStatusPresent {
		t.Fatalf("one-label status = %q, want present", statuses[oneLabel.ID].Status)
	}
	if statuses[twoLabels.ID].Status != CertificateStatusAbsent {
		t.Fatalf("two-label status = %q, want absent (a wildcard covers exactly one label)", statuses[twoLabels.ID].Status)
	}
}

func TestCertificateStatusesNeverFailsOnStoreError(t *testing.T) {
	certificate := statusCertificate("app.example.com")
	store := &fakeStatusStore{err: errors.New("database down")}
	svc := NewDefaultCertificateStatusService(CertificateStatusConfig{Store: store, Dial: func(context.Context, uuid.UUID) (ACMEReader, error) {
		t.Fatal("dial must not run when the target listing fails")
		return nil, nil
	}, Logger: discardLogger()})

	statuses := svc.CertificateStatuses(context.Background(), []DomainCertificate{certificate})
	if statuses[certificate.ID].Status != CertificateStatusUnknown {
		t.Fatalf("status = %q, want unknown", statuses[certificate.ID].Status)
	}
}

func TestCertificateStatusesEmptyInput(t *testing.T) {
	svc := NewDefaultCertificateStatusService(CertificateStatusConfig{
		Store:  &fakeStatusStore{},
		Dial:   func(context.Context, uuid.UUID) (ACMEReader, error) { return nil, errors.New("unused") },
		Logger: discardLogger(),
	})
	if got := svc.CertificateStatuses(context.Background(), nil); len(got) != 0 {
		t.Fatalf("statuses = %v, want empty", got)
	}
}

func TestNewDefaultCertificateStatusServiceDisabledWithoutStore(t *testing.T) {
	if svc := NewDefaultCertificateStatusService(CertificateStatusConfig{}); svc != nil {
		t.Fatal("service without a store must be nil")
	}
}
