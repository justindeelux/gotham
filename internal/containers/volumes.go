package containers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// ErrVolumeListUnsupported is returned when a node agent predates ListVolumes.
var ErrVolumeListUnsupported = errors.New("containers: the node agent does not support volume listings; update the node agent")

// ErrVolumeInUse is returned when Docker refuses to remove a volume a
// container still mounts.
var ErrVolumeInUse = errors.New("containers: volume is in use")

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	databaseVolumePattern    = regexp.MustCompile(`^gotham-db-(` + uuidPattern + `)$`)
	applicationVolumePattern = regexp.MustCompile(`^gotham-app-(` + uuidPattern + `)-[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	composeVolumePattern     = regexp.MustCompile(`^gotham-(` + uuidPattern + `)_[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

// VolumeOwners is the set of resources recorded in the control plane that own
// Docker volumes by name.
type VolumeOwners struct {
	Applications map[uuid.UUID]struct{}
	Services     map[uuid.UUID]struct{}
	Databases    map[string]struct{}
}

// NewVolumeOwners indexes the recorded owners.
func NewVolumeOwners(applications, services []uuid.UUID, databases []string) VolumeOwners {
	owners := VolumeOwners{
		Applications: make(map[uuid.UUID]struct{}, len(applications)),
		Services:     make(map[uuid.UUID]struct{}, len(services)),
		Databases:    make(map[string]struct{}, len(databases)),
	}
	for _, id := range applications {
		owners.Applications[id] = struct{}{}
	}
	for _, id := range services {
		owners.Services[id] = struct{}{}
	}
	for _, name := range databases {
		owners.Databases[name] = struct{}{}
	}
	return owners
}

// VolumeOwnerSource loads the recorded owners. It fails closed: a control
// plane that cannot read its records must not offer any volume for removal.
type VolumeOwnerSource func(ctx context.Context) (VolumeOwners, error)

// OrphanedVolume is one node volume no recorded resource owns.
type OrphanedVolume struct {
	Name      string     `json:"name"`
	SizeBytes int64      `json:"size_bytes"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// Owns reports whether a recorded resource owns the named Docker volume.
func (o VolumeOwners) Owns(name string) bool {
	if _, ok := o.Databases[name]; ok && databaseVolumePattern.MatchString(name) {
		return true
	}
	if match := applicationVolumePattern.FindStringSubmatch(name); match != nil {
		return o.hasApplication(match[1])
	}
	if match := composeVolumePattern.FindStringSubmatch(name); match != nil {
		return o.hasApplication(match[1]) || o.hasService(match[1])
	}
	return false
}

func (o VolumeOwners) hasApplication(raw string) bool {
	id, err := uuid.Parse(raw)
	if err != nil {
		return false
	}
	_, ok := o.Applications[id]
	return ok
}

func (o VolumeOwners) hasService(raw string) bool {
	id, err := uuid.Parse(raw)
	if err != nil {
		return false
	}
	_, ok := o.Services[id]
	return ok
}

// isManagedVolumeName reports whether Gotham could have created the volume.
// Custom compose names and volumes created outside Gotham never match, so the
// node view never offers them for removal.
func isManagedVolumeName(name string) bool {
	return databaseVolumePattern.MatchString(name) ||
		applicationVolumePattern.MatchString(name) ||
		composeVolumePattern.MatchString(name)
}

// VolumeService lists and removes the volumes no recorded resource owns.
type VolumeService interface {
	ListOrphanedVolumes(ctx context.Context, serverID uuid.UUID) ([]OrphanedVolume, error)
	RemoveOrphanedVolume(ctx context.Context, serverID uuid.UUID, name string) error
}

var _ VolumeService = (*Service)(nil)

func (s *Service) SetVolumeOwners(source VolumeOwnerSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volumeOwners = source
}

func (s *Service) owners(ctx context.Context) (VolumeOwners, error) {
	s.mu.RLock()
	source := s.volumeOwners
	s.mu.RUnlock()
	if source == nil {
		return VolumeOwners{}, errors.New("containers: volume ownership is not configured")
	}
	owners, err := source(ctx)
	if err != nil {
		return VolumeOwners{}, fmt.Errorf("containers: load volume owners: %w", err)
	}
	return owners, nil
}

func (s *Service) ListOrphanedVolumes(ctx context.Context, serverID uuid.UUID) ([]OrphanedVolume, error) {
	owners, err := s.owners(ctx)
	if err != nil {
		return nil, err
	}
	ctx, client, cancel, err := s.client(ctx, serverID, false)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.ListVolumes(ctx, &agentv1.ListVolumesRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, ErrVolumeListUnsupported
		}
		return nil, mapRPCError(err)
	}
	volumes := make([]OrphanedVolume, 0, len(response.GetVolumes()))
	for _, info := range response.GetVolumes() {
		name := info.GetName()
		if !isManagedVolumeName(name) || owners.Owns(name) || info.GetRefCount() > 0 {
			continue
		}
		volume := OrphanedVolume{Name: name, SizeBytes: info.GetSizeBytes()}
		if created := info.GetCreatedAt(); created != nil && created.IsValid() {
			timestamp := created.AsTime().UTC()
			volume.CreatedAt = &timestamp
		}
		volumes = append(volumes, volume)
	}
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].Name < volumes[j].Name })
	return volumes, nil
}

func (s *Service) RemoveOrphanedVolume(ctx context.Context, serverID uuid.UUID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: volume name is required", ErrValidation)
	}
	if !isManagedVolumeName(name) {
		return fmt.Errorf("%w: %q is not a Gotham-managed volume", ErrForbidden, name)
	}
	owners, err := s.owners(ctx)
	if err != nil {
		return err
	}
	if owners.Owns(name) {
		return fmt.Errorf("%w: %q belongs to a recorded resource", ErrForbidden, name)
	}
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	if _, err := client.RemoveVolume(ctx, &agentv1.VolumeActionRequest{Name: name}); err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			return fmt.Errorf("%w: %s", ErrVolumeInUse, status.Convert(err).Message())
		}
		return mapRPCError(err)
	}
	return nil
}
