package proxy

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// CertificateStatus is the observed state of one certificate intent against
// its node's ACME storage. It is computed on read and never stored: the
// control plane has no issuance telemetry of its own, so it reports what the
// node's storage actually contains, and unknown whenever that cannot be read.
type CertificateStatus string

const (
	// CertificateStatusPresent means the node's storage holds a certificate
	// covering the intent's recorded domain (its main name or one of its
	// SANs, including a one-label wildcard SAN).
	CertificateStatusPresent CertificateStatus = "present"
	// CertificateStatusAbsent means the node's storage was read and holds no
	// certificate covering the domain.
	CertificateStatusAbsent CertificateStatus = "absent"
	// CertificateStatusUnknown means the node or its storage could not be
	// read (unreachable, error, malformed), so no claim is possible.
	CertificateStatusUnknown CertificateStatus = "unknown"
)

// CertificateStatusObservation is the observed state of one certificate
// intent. NotAfter is the covering certificate's expiry, zero unless the
// status is present.
type CertificateStatusObservation struct {
	Status   CertificateStatus
	NotAfter time.Time
}

// CertificateStatusService observes the node ACME storage behind certificate
// intents. Implementations must never fail a read because a node is
// unreachable or its storage is unreadable: those conditions are reported as
// unknown, so the certificate API can never be turned into an error by node
// state.
type CertificateStatusService interface {
	// CertificateStatuses returns one observation per given certificate,
	// keyed by certificate id. The returned map always has an entry for
	// every input certificate.
	CertificateStatuses(ctx context.Context, certificates []DomainCertificate) map[uuid.UUID]CertificateStatusObservation
}

// CertificateStatusTarget pairs one certificate intent with the node hosting
// its application. ServerID is uuid.Nil when the application is unassigned;
// such a certificate is always unknown.
type CertificateStatusTarget struct {
	CertificateID uuid.UUID
	Domain        string
	ServerID      uuid.UUID
}

// CertificateStatusStore is the persistence seam of the status service.
type CertificateStatusStore interface {
	ListCertificateStatusTargets(ctx context.Context) ([]CertificateStatusTarget, error)
}

// storeCertificateStatus adapts the shared store to the status seam.
type storeCertificateStatus struct {
	store *store.Store
}

// NewStoreCertificateStatus adapts the shared store to the certificate status
// seam. It is exported so internal/server can build the production status
// service over one store.
func NewStoreCertificateStatus(st *store.Store) CertificateStatusStore {
	return storeCertificateStatus{store: st}
}

// ListCertificateStatusTargets maps the joined rows.
func (s storeCertificateStatus) ListCertificateStatusTargets(ctx context.Context) ([]CertificateStatusTarget, error) {
	rows, err := s.store.ListCertificateStatusTargets(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]CertificateStatusTarget, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, CertificateStatusTarget{
			CertificateID: uuidFromPG(row.CertificateID),
			Domain:        row.Domain,
			ServerID:      uuidFromPG(row.ServerID),
		})
	}
	return targets, nil
}

// ACMEReader is the node agent's ReadACMEStorage as the control plane uses it.
// *servers.ProxyClient satisfies it; tests substitute a fake.
type ACMEReader interface {
	ReadACMEStorage(ctx context.Context, in *agentv1.ReadACMEStorageRequest, opts ...grpc.CallOption) (*agentv1.ReadACMEStorageResponse, error)
	Close() error
}

// StatusDialFunc opens the agent ProxyService of one node for a status read. It
// is satisfied by *servers.ServerService through the internal/server wiring.
type StatusDialFunc func(ctx context.Context, serverID uuid.UUID) (ACMEReader, error)

// CertificateStatusConfig wires the status service. Store and Dial are
// required; the timeout bounds one node read.
type CertificateStatusConfig struct {
	// Store is the status persistence seam.
	Store CertificateStatusStore
	// Dial opens the node agent's ProxyService. A nil dialer makes every
	// observation unknown (never an error).
	Dial StatusDialFunc
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Timeout bounds one node's storage read; default 10 seconds.
	Timeout time.Duration
}

// defaultStatusTimeout bounds one node's ACME storage read.
const defaultStatusTimeout = 10 * time.Second

// errStatusDialerUnconfigured is an internal symptom only: it is reported as
// unknown, never returned to a caller.
var errStatusDialerUnconfigured = errors.New("proxy: agent dialer is not configured")

// certificateStatusService computes per-intent observations by reading each
// node's ACME storage once per request.
type certificateStatusService struct {
	store   CertificateStatusStore
	dial    StatusDialFunc
	logger  *slog.Logger
	timeout time.Duration
}

// Compile-time guarantee that certificateStatusService satisfies the contract.
var _ CertificateStatusService = (*certificateStatusService)(nil)

// NewDefaultCertificateStatusService builds the production certificate status
// service, or nil when there is no store, no dialer or FEATURE_PROXY=false, so
// the HTTP wiring can pass its result to Mount unconditionally.
func NewDefaultCertificateStatusService(cfg CertificateStatusConfig) CertificateStatusService {
	if cfg.Store == nil || !Enabled() {
		return nil
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultStatusTimeout
	}
	return &certificateStatusService{store: cfg.Store, dial: cfg.Dial, logger: logger, timeout: timeout}
}

// CertificateStatuses observes the node storage for the given certificates.
// Every certificate gets an entry: unknown unless a node read succeeded, and
// present/absent from the covering-certificate search. It deliberately has no
// error return — a node failure must never turn the certificate list into an
// error (BE-6.3).
func (s *certificateStatusService) CertificateStatuses(ctx context.Context, certificates []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	statuses := make(map[uuid.UUID]CertificateStatusObservation, len(certificates))
	for _, certificate := range certificates {
		statuses[certificate.ID] = CertificateStatusObservation{Status: CertificateStatusUnknown}
	}
	if s == nil || len(certificates) == 0 {
		return statuses
	}

	targets, err := s.store.ListCertificateStatusTargets(ctx)
	if err != nil {
		s.logger.Warn("proxy: list certificate status targets", "error", err)
		return statuses
	}
	byCertificate := make(map[uuid.UUID]CertificateStatusTarget, len(targets))
	for _, target := range targets {
		byCertificate[target.CertificateID] = target
	}

	// One read per node, shared by every certificate on it.
	type request struct {
		certificate DomainCertificate
		domain      string
	}
	byServer := make(map[uuid.UUID][]request)
	for _, certificate := range certificates {
		target, ok := byCertificate[certificate.ID]
		if !ok || target.ServerID == uuid.Nil {
			continue
		}
		domain := target.Domain
		if domain == "" {
			domain = certificate.Domain
		}
		byServer[target.ServerID] = append(byServer[target.ServerID], request{certificate: certificate, domain: domain})
	}

	serverIDs := make([]uuid.UUID, 0, len(byServer))
	for serverID := range byServer {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Slice(serverIDs, func(i, j int) bool { return serverIDs[i].String() < serverIDs[j].String() })

	for _, serverID := range serverIDs {
		entries := byServer[serverID]
		stored, present, err := s.readStorage(ctx, serverID)
		if err != nil {
			// Node unreachable, RPC failed or storage unreadable: every
			// certificate on the node stays unknown, and the failure is a
			// debug fact, not an API error.
			s.logger.Debug("proxy: certificate status read failed",
				"server_id", serverID.String(), "error", err)
			continue
		}
		if !present {
			for _, entry := range entries {
				statuses[entry.certificate.ID] = CertificateStatusObservation{Status: CertificateStatusAbsent}
			}
			continue
		}
		for _, entry := range entries {
			if notAfter, ok := coveringNotAfter(entry.domain, stored); ok {
				statuses[entry.certificate.ID] = CertificateStatusObservation{Status: CertificateStatusPresent, NotAfter: notAfter}
			} else {
				statuses[entry.certificate.ID] = CertificateStatusObservation{Status: CertificateStatusAbsent}
			}
		}
	}
	return statuses
}

// readStorage opens the node agent and reads its ACME storage. present
// reports whether a storage file exists; a read failure is returned as an
// error.
func (s *certificateStatusService) readStorage(ctx context.Context, serverID uuid.UUID) (certificates []*agentv1.ACMECertificateInfo, present bool, err error) {
	if s.dial == nil {
		return nil, false, errStatusDialerUnconfigured
	}
	readCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	client, err := s.dial(readCtx, serverID)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Debug("proxy: close acme reader", "server_id", serverID.String(), "error", closeErr)
		}
	}()
	response, err := client.ReadACMEStorage(readCtx, &agentv1.ReadACMEStorageRequest{})
	if err != nil {
		return nil, false, err
	}
	return response.GetCertificates(), response.GetPresent(), nil
}

// coveringNotAfter returns the latest expiry among the stored certificates
// that cover domain (exact main or SAN, or a one-label wildcard SAN).
func coveringNotAfter(domain string, stored []*agentv1.ACMECertificateInfo) (time.Time, bool) {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return time.Time{}, false
	}
	var best time.Time
	for _, certificate := range stored {
		if !certificateCovers(certificate, domain) {
			continue
		}
		notAfter := certificate.GetNotAfter().AsTime()
		if notAfter.IsZero() {
			continue
		}
		if best.IsZero() || notAfter.After(best) {
			best = notAfter
		}
	}
	return best, !best.IsZero()
}

// certificateCovers reports whether one stored certificate's names cover the
// exact host: the main name, a SAN, or a one-label wildcard SAN.
func certificateCovers(certificate *agentv1.ACMECertificateInfo, domain string) bool {
	if NormalizeDomain(certificate.GetMain()) == domain {
		return true
	}
	for _, san := range certificate.GetSans() {
		normalized := NormalizeDomain(san)
		if normalized == domain {
			return true
		}
		if base, ok := strings.CutPrefix(normalized, "*."); ok {
			// A wildcard matches exactly one label: a.example.com is covered
			// by *.example.com, a.b.example.com is not.
			if strings.HasSuffix(domain, "."+base) && strings.Count(domain, ".") == strings.Count(base, ".")+1 {
				return true
			}
		}
	}
	return false
}
