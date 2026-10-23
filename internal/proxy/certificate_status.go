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

type CertificateStatus string

const (
	CertificateStatusPresent CertificateStatus = "present"
	CertificateStatusAbsent  CertificateStatus = "absent"
	CertificateStatusUnknown CertificateStatus = "unknown"
)

type CertificateStatusObservation struct {
	Status   CertificateStatus
	NotAfter time.Time
}

type CertificateStatusService interface {
	CertificateStatuses(ctx context.Context, certificates []DomainCertificate) map[uuid.UUID]CertificateStatusObservation
}

type CertificateStatusTarget struct {
	CertificateID uuid.UUID
	Domain        string
	ServerID      uuid.UUID
}

type CertificateStatusStore interface {
	ListCertificateStatusTargets(ctx context.Context) ([]CertificateStatusTarget, error)
}

type storeCertificateStatus struct {
	store *store.Store
}

func NewStoreCertificateStatus(st *store.Store) CertificateStatusStore {
	return storeCertificateStatus{store: st}
}

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

type ACMEReader interface {
	ReadACMEStorage(ctx context.Context, in *agentv1.ReadACMEStorageRequest, opts ...grpc.CallOption) (*agentv1.ReadACMEStorageResponse, error)
	Close() error
}

type StatusDialFunc func(ctx context.Context, serverID uuid.UUID) (ACMEReader, error)

type CertificateStatusConfig struct {
	Store   CertificateStatusStore
	Dial    StatusDialFunc
	Logger  *slog.Logger
	Timeout time.Duration
}

const defaultStatusTimeout = 10 * time.Second

var errStatusDialerUnconfigured = errors.New("proxy: agent dialer is not configured")

type certificateStatusService struct {
	store   CertificateStatusStore
	dial    StatusDialFunc
	logger  *slog.Logger
	timeout time.Duration
}

var _ CertificateStatusService = (*certificateStatusService)(nil)

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

func certificateCovers(certificate *agentv1.ACMECertificateInfo, domain string) bool {
	if certificateNameCovers(certificate.GetMain(), domain) {
		return true
	}
	for _, san := range certificate.GetSans() {
		if certificateNameCovers(san, domain) {
			return true
		}
	}
	return false
}

func certificateNameCovers(name, domain string) bool {
	name = NormalizeDomain(name)
	if name == domain {
		return true
	}
	base, ok := strings.CutPrefix(name, "*.")
	if !ok {
		return false
	}
	return strings.HasSuffix(domain, "."+base) && strings.Count(domain, ".") == strings.Count(base, ".")+1
}
