package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// FeatureEnv is the kill switch for the whole file-manager surface:
// FEATURE_FILES=true enables it. It defaults off (unlike the databases and
// services flags, which default on) until step 10 signs the feature off: the
// routes are mounted but every route answers 503 ErrDisabled while the flag is
// off.
const FeatureEnv = "FEATURE_FILES"

// Enabled reports whether the file-manager feature is on. It defaults off: only
// an explicit true turns it on, so an unconfigured control plane cannot expose
// file bodies by accident.
func Enabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "true")
}

const (
	// defaultRPCTimeout bounds the unary read RPCs (roots/list/stat) and the
	// Content read.
	defaultRPCTimeout = 30 * time.Second
	// defaultTransferTimeout bounds a Download stream. A large file transfer
	// outlives the 30 s HTTP request budget the plan calls out, so the stream
	// gets its own, much larger budget; the request context still cancels it
	// the moment the client disconnects.
	defaultTransferTimeout = 30 * time.Minute
	// defaultTicketTTL is the download ticket lifetime.
	defaultTicketTTL = 60 * time.Second
)

// MaxContentBytes is the largest file the editor may load over GET /content.
// Larger files answer 413 and are offered as a download instead.
const MaxContentBytes int64 = 2 << 20 // 2 MiB

// Root is one browsable root as reported by the agent.
type Root struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Writable  bool   `json:"writable"`
	FreeBytes uint64 `json:"free_bytes"`
}

// Entry is one directory entry.
type Entry struct {
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Size          int64     `json:"size"`
	Mode          uint32    `json:"mode"`
	ModifiedAt    time.Time `json:"modified_at"`
	SymlinkTarget string    `json:"symlink_target,omitempty"`
	Etag          string    `json:"etag"`
}

// Listing is one page of a directory listing.
type Listing struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
	Cursor    string  `json:"cursor,omitempty"`
}

// Content is one editable text body.
type Content struct {
	Content string `json:"content"`
	Etag    string `json:"etag"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
}

// Download is an open agent read stream. Chunks is closed when the stream ends;
// Err carries a single terminal error when the stream failed before a clean
// end, so a caller never mistakes a truncated download for success.
type Download struct {
	Name   string
	Size   int64
	Etag   string
	Chunks <-chan []byte
	Err    <-chan error
}

// FileService is the control-plane read-side API. Every method authorizes the
// target server before touching the agent (see resolve).
type FileService interface {
	Roots(ctx context.Context, serverID uuid.UUID) ([]Root, error)
	List(ctx context.Context, serverID uuid.UUID, root, path, cursor string) (Listing, error)
	Stat(ctx context.Context, serverID uuid.UUID, root, path string) (Entry, error)
	Content(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error)
	// IssueDownloadTicket authorizes an owner/admin read of (root, path) and
	// returns a single-use, short-lived ticket for the unauthenticated
	// download route (a native browser download cannot send the bearer token).
	IssueDownloadTicket(ctx context.Context, serverID uuid.UUID, root, path string) (string, error)
	Download(ctx context.Context, serverID uuid.UUID, ticket string) (*Download, error)
}

// Config wires a Service. Registry is required; Dial may be nil until the mTLS
// dialer is plugged in (see Service.SetDial).
type Config struct {
	Registry        Registry
	Dial            DialFunc
	Logger          *slog.Logger
	RPCTimeout      time.Duration
	TransferTimeout time.Duration
	TicketTTL       time.Duration
	// MaxContentBytes overrides the editor ceiling; 0 keeps MaxContentBytes.
	MaxContentBytes int64
	// Now overrides the clock the ticket store uses; nil uses time.Now.
	Now func() time.Time
}

// Service is the control-plane file domain service.
type Service struct {
	mu              sync.RWMutex
	registry        Registry
	dial            DialFunc
	logger          *slog.Logger
	rpcTimeout      time.Duration
	transferTimeout time.Duration
	maxContentBytes int64
	tickets         *ticketStore
}

// NewService builds a Service, applying defaults for the optional fields.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	rpcTimeout := cfg.RPCTimeout
	if rpcTimeout <= 0 {
		rpcTimeout = defaultRPCTimeout
	}
	transferTimeout := cfg.TransferTimeout
	if transferTimeout <= 0 {
		transferTimeout = defaultTransferTimeout
	}
	maxContent := cfg.MaxContentBytes
	if maxContent <= 0 {
		maxContent = MaxContentBytes
	}
	ttl := cfg.TicketTTL
	if ttl <= 0 {
		ttl = defaultTicketTTL
	}
	return &Service{
		registry:        cfg.Registry,
		dial:            cfg.Dial,
		logger:          logger,
		rpcTimeout:      rpcTimeout,
		transferTimeout: transferTimeout,
		maxContentBytes: maxContent,
		tickets:         newTicketStore(ttl, cfg.Now),
	}
}

// NewDefaultService builds the production Service for the HTTP wiring. It
// returns nil when registry is nil so callers can skip mounting the routes.
func NewDefaultService(registry Registry) *Service {
	if registry == nil {
		return nil
	}
	return NewService(Config{Registry: registry})
}

// SetDial installs (or replaces) the agent dialer. It is the merge-time seam
// for the mTLS dial.
func (s *Service) SetDial(dial DialFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dial = dial
}

// Roots returns the node's allow-listed roots. Any team member may read them.
func (s *Service) Roots(ctx context.Context, serverID uuid.UUID) ([]Root, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.ListRoots(ctx, &agentv1.ListRootsRequest{})
	if err != nil {
		return nil, mapRPCError(err)
	}
	roots := make([]Root, 0, len(response.GetRoots()))
	for _, info := range response.GetRoots() {
		roots = append(roots, newRoot(info))
	}
	return roots, nil
}

// List returns one directory's entries. Any team member may read them.
func (s *Service) List(ctx context.Context, serverID uuid.UUID, root, path, cursor string) (Listing, error) {
	if !Enabled() {
		return Listing{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Listing{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return Listing{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.List(ctx, &agentv1.ListRequest{Root: root, Path: path, Cursor: cursor})
	if err != nil {
		return Listing{}, mapRPCError(err)
	}
	entries := make([]Entry, 0, len(response.GetEntries()))
	for _, info := range response.GetEntries() {
		entries = append(entries, newEntry(info))
	}
	return Listing{Entries: entries, Truncated: response.GetTruncated(), Cursor: response.GetCursor()}, nil
}

// Stat returns one entry. Any team member may read it.
func (s *Service) Stat(ctx context.Context, serverID uuid.UUID, root, path string) (Entry, error) {
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: path})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	entry := response.GetEntry()
	if entry == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(entry), nil
}

// Content reads one file body for the editor. Owner/admin only: file bodies
// hold secrets such as 0600 compose environment files. A file larger than the
// editor ceiling answers ErrTooLarge (413) and a binary/invalid-UTF-8 body
// answers ErrNotEditable (415); both should be offered as a download instead.
func (s *Service) Content(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error) {
	if !Enabled() {
		return Content{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Content{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, true)
	if err != nil {
		return Content{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: path})
	if err != nil {
		return Content{}, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		return Content{}, ErrNotFound
	}
	if info.GetSize() > s.maxContentBytes {
		return Content{}, ErrTooLarge
	}

	stream, err := client.Read(ctx, &agentv1.ReadRequest{Root: root, Path: path})
	if err != nil {
		return Content{}, mapRPCError(err)
	}
	body, err := readAllLimited(stream, s.maxContentBytes)
	if err != nil {
		return Content{}, err
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return Content{}, ErrNotEditable
	}
	sum := sha256.Sum256(body)
	return Content{
		Content: string(body),
		Etag:    info.GetEtag(),
		SHA256:  hex.EncodeToString(sum[:]),
		Size:    int64(len(body)),
		Mode:    info.GetMode(),
	}, nil
}

// IssueDownloadTicket authorizes an owner/admin read of (root, path) and returns
// a single-use, short-lived ticket. The download route is unauthenticated by
// design: a native browser download cannot attach the bearer token, so the
// ticket is the credential and it carries the user/team/server it was issued to.
func (s *Service) IssueDownloadTicket(ctx context.Context, serverID uuid.UUID, root, path string) (string, error) {
	if !Enabled() {
		return "", ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return "", err
	}
	server, err := s.resolve(ctx, serverID, true)
	if err != nil {
		return "", err
	}
	scope := teams.ScopeFor(ctx, uuid.Nil)
	return s.tickets.issue(ticket{
		UserID:   scope.UserID,
		TeamID:   server.TeamID,
		ServerID: serverID,
		Root:     root,
		Path:     path,
	}), nil
}

// Download consumes a ticket and opens the agent read stream for the entry. The
// ticket is the authorization: it was issued to an owner/admin and is bound to
// one (server, root, path). A missing, expired or already-consumed ticket, or a
// ticket for a different server, answers ErrNotFound.
func (s *Service) Download(ctx context.Context, serverID uuid.UUID, ticketID string) (*Download, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	t, ok := s.tickets.consume(ticketID)
	if !ok {
		return nil, ErrNotFound
	}
	if t.ServerID != serverID {
		return nil, ErrNotFound
	}
	server, err := s.server(ctx, serverID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, s.transferTimeout)
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, err
	}

	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: t.Root, Path: t.Path})
	if err != nil {
		s.closeClient(client)
		cancel()
		return nil, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		s.closeClient(client)
		cancel()
		return nil, ErrNotFound
	}

	stream, err := client.Read(ctx, &agentv1.ReadRequest{Root: t.Root, Path: t.Path})
	if err != nil {
		s.closeClient(client)
		cancel()
		return nil, mapRPCError(err)
	}

	chunks := make(chan []byte)
	streamErr := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(streamErr)
		defer cancel()
		defer s.closeClient(client)
		for {
			chunk, err := stream.Recv()
			if err != nil {
				// io.EOF is the normal end of a read stream. Any other error
				// while ctx is still live is a failed stream and is surfaced so
				// the handler aborts the connection instead of ending the body
				// early and looking like a short (but successful) download.
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					streamErr <- mapRPCError(err)
					s.logger.Debug("files: download stream ended early", "error", err)
				}
				return
			}
			data := chunk.GetData()
			if len(data) == 0 {
				continue
			}
			select {
			case chunks <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	return &Download{
		Name:   info.GetName(),
		Size:   info.GetSize(),
		Etag:   info.GetEtag(),
		Chunks: chunks,
		Err:    streamErr,
	}, nil
}

// resolve returns the registry entry for a server after authorizing the caller.
// Unlike the rest of the node surface (AuthorizeOptionalTeam), a browsable
// server must belong to a team: a legacy team-less node has no team to
// authorize against and answers ErrNotFound, so file bodies are never shared
// with every authenticated caller. write requires an owner/admin role; the
// remaining roles read. A request without a team scope (teams not wired, a
// non-HTTP caller) keeps the pre-teams compatibility path.
func (s *Service) resolve(ctx context.Context, serverID uuid.UUID, write bool) (*servers.Server, error) {
	server, err := s.server(ctx, serverID)
	if err != nil {
		return nil, err
	}
	scope := teams.ScopeFor(ctx, uuid.Nil)
	if !scope.Active() {
		return server, nil
	}
	if server.TeamID == uuid.Nil {
		return nil, ErrNotFound
	}
	if err := scope.AuthorizeResource(server.TeamID, uuid.Nil, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return nil, ErrForbidden
		}
		return nil, ErrNotFound
	}
	return server, nil
}

// server resolves a server ID to its registry entry without an RBAC check. It
// is used by Download, where the ticket is the authorization.
func (s *Service) server(ctx context.Context, serverID uuid.UUID) (*servers.Server, error) {
	if s.registry == nil {
		return nil, errors.New("files: server registry is not configured")
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("files: resolve server: %w", err)
	}
	return server, nil
}

// rpc resolves and dials a server, returning the bounded context the caller
// must pass to the RPC. write selects the authorization: read methods pass
// false, content and the download ticket pass true.
func (s *Service) rpc(ctx context.Context, serverID uuid.UUID, write bool) (context.Context, FileClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.rpcTimeout)
	server, err := s.resolve(ctx, serverID, write)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return ctx, client, cancel, nil
}

// dialClient opens the agent client, reporting a clear error while no dialer is
// configured.
func (s *Service) dialClient(ctx context.Context, server *servers.Server) (FileClient, error) {
	s.mu.RLock()
	dial := s.dial
	s.mu.RUnlock()
	if dial == nil {
		return nil, fmt.Errorf("%w: agent dialer is not configured", ErrAgentUnavailable)
	}
	client, err := dial(ctx, server)
	if err != nil {
		return nil, mapRPCError(err)
	}
	if client == nil {
		return nil, fmt.Errorf("%w: agent dial returned no client", ErrAgentUnavailable)
	}
	return client, nil
}

// closeClient releases a dialed agent client. The production client owns a gRPC
// connection, so every operation closes what it dialed; a test fake that does
// not implement io.Closer is left untouched.
func (s *Service) closeClient(client FileClient) {
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		s.logger.Debug("files: closing agent client failed", "error", err)
	}
}

// validateRoot rejects an empty root name before it reaches the agent.
func validateRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("%w: root is required", ErrValidation)
	}
	return nil
}

// readAllLimited streams a body, failing with ErrTooLarge once it exceeds limit
// bytes. A file that grows past the ceiling between the Stat and the read is
// still refused.
func readAllLimited(stream grpc.ServerStreamingClient[agentv1.ReadChunk], limit int64) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, mapRPCError(err)
		}
		data := chunk.GetData()
		if int64(buf.Len())+int64(len(data)) > limit {
			return nil, ErrTooLarge
		}
		buf.Write(data)
	}
}

// newRoot maps the agent's FileRoot onto the API DTO.
func newRoot(info *agentv1.FileRoot) Root {
	return Root{
		Name:      info.GetName(),
		Path:      info.GetPath(),
		Writable:  info.GetWritable(),
		FreeBytes: info.GetFreeBytes(),
	}
}

// newEntry maps the agent's FileEntry onto the API DTO.
func newEntry(info *agentv1.FileEntry) Entry {
	entry := Entry{
		Name:          info.GetName(),
		Kind:          kindString(info.GetKind()),
		Size:          info.GetSize(),
		Mode:          info.GetMode(),
		SymlinkTarget: info.GetSymlinkTarget(),
		Etag:          info.GetEtag(),
	}
	if ts := info.GetModifiedAt(); ts != nil {
		entry.ModifiedAt = ts.AsTime()
	}
	return entry
}

// kindString maps the agent's FileKind enum onto the stable API string.
func kindString(kind agentv1.FileKind) string {
	switch kind {
	case agentv1.FileKind_FILE_KIND_FILE:
		return "file"
	case agentv1.FileKind_FILE_KIND_DIRECTORY:
		return "directory"
	case agentv1.FileKind_FILE_KIND_SYMLINK:
		return "symlink"
	case agentv1.FileKind_FILE_KIND_SPECIAL:
		return "special"
	default:
		return "unspecified"
	}
}

// mapRPCError translates agent transport failures to service sentinels;
// unrecognised errors pass through unchanged.
func mapRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %v", ErrValidation, err)
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %v", ErrForbidden, err)
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case codes.ResourceExhausted:
		return fmt.Errorf("%w: %v", ErrBusy, err)
	case codes.Unimplemented:
		return fmt.Errorf("%w: %v", ErrAgentOutdated, err)
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	default:
		return err
	}
}

// ticket is one download grant: single-use, short-lived, and bound to the
// user/team/server/root/path it was issued for.
type ticket struct {
	UserID   uuid.UUID
	TeamID   uuid.UUID
	ServerID uuid.UUID
	Root     string
	Path     string
	Expires  time.Time
}

// ticketStore is the in-memory registry of outstanding download tickets. It is
// deliberately not persisted: a ticket is valid for 60 s and a control-plane
// restart simply invalidates the outstanding ones.
type ticketStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	tickets map[string]ticket
}

// newTicketStore builds a store with the given lifetime and clock.
func newTicketStore(ttl time.Duration, now func() time.Time) *ticketStore {
	if now == nil {
		now = time.Now
	}
	return &ticketStore{ttl: ttl, now: now, tickets: make(map[string]ticket)}
}

// issue mints a ticket and returns its opaque token.
func (s *ticketStore) issue(t ticket) string {
	token := newTicketID()
	t.Expires = s.now().Add(s.ttl)
	s.mu.Lock()
	s.pruneLocked()
	s.tickets[token] = t
	s.mu.Unlock()
	return token
}

// consume removes and returns the ticket for token. It reports false when the
// token is unknown, expired or already consumed, so a ticket is single-use.
func (s *ticketStore) consume(token string) (ticket, bool) {
	if token == "" {
		return ticket{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	t, ok := s.tickets[token]
	if !ok {
		return ticket{}, false
	}
	delete(s.tickets, token)
	return t, true
}

// pruneLocked drops expired tickets. The map is tiny (one entry per outstanding
// download) so a full scan on each issue/consume is cheaper than a timer.
func (s *ticketStore) pruneLocked() {
	now := s.now()
	for token, t := range s.tickets {
		if !now.Before(t.Expires) {
			delete(s.tickets, token)
		}
	}
}

// newTicketID returns a 256-bit URL-safe random token.
func newTicketID() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand never fails on a supported platform; a panic here is
		// preferable to handing out a guessable ticket.
		panic(fmt.Sprintf("files: generate download ticket: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}
