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
	"strconv"
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
// FEATURE_FILES=false disables it. It defaults on (like the databases and
// services flags); the surface is limited to platform operators, and a node only
// answers while its agent has the file service on (GOTHAM_AGENT_FILES). While the flag is
// off every route answers 503 ErrDisabled.
const FeatureEnv = "FEATURE_FILES"

// Enabled reports whether the file-manager feature is on. Only an explicit
// false turns it off.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// MaxUploadEnv optionally overrides the write/upload body ceiling in bytes; an
// unset or invalid value keeps DefaultMaxUploadBytes. It is a control-plane
// guard, independent of the agent's own GOTHAM_AGENT_FILE_MAX_UPLOAD.
const MaxUploadEnv = "FILES_MAX_UPLOAD_BYTES"

// maxUploadFromEnv parses MaxUploadEnv; 0 lets NewService apply the default.
func maxUploadFromEnv() int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(MaxUploadEnv)), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
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

// MaxContentBytes is the largest file the editor loads in full over
// GET /content. A larger file answers a read-only tail instead (see
// TailLines); saving one is refused.
const MaxContentBytes int64 = 2 << 20 // 2 MiB

// TailLines caps the read-only tail view of a file over MaxContentBytes. The
// tail is also bounded by MaxContentBytes, so a file of very long lines may
// show fewer than TailLines lines.
const TailLines = 1000

// maxFileArgBytes bounds a root/path/cursor query value before it is put in a
// proto message. A value longer than any real path is refused at the control
// plane rather than passed to the agent.
const maxFileArgBytes = 4096

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
	// Owner and Group are the owning user and group names reported by the
	// agent; an unresolvable id is kept as its numeric string. They are empty
	// from an agent that predates these fields.
	Owner string `json:"owner"`
	Group string `json:"group"`
}

// Listing is one page of a directory listing.
type Listing struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
	Cursor    string  `json:"cursor,omitempty"`
}

// TrashEntry is one entry held in a node's recycle bin.
type TrashEntry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OriginalPath string    `json:"original_path"`
	DeletedAt    time.Time `json:"deleted_at"`
	Size         int64     `json:"size"`
	Kind         string    `json:"kind"`
}

// Content is one editable text body.
type Content struct {
	Content string `json:"content"`
	// Encoding is the detected charset of Content (utf-8, ascii, gbk, gb2312,
	// big5). The editor shows it and sends it back on save, so an unedited file
	// is written with the same bytes. ASCII-only bodies are reported as UTF-8.
	Encoding string `json:"encoding"`
	Etag     string `json:"etag"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Mode     uint32 `json:"mode"`
	// Truncated reports a read-only tail view: Content is the end of a file
	// larger than the editor ceiling, Size is the whole file, and SHA256 covers
	// only the returned tail. A truncated body is never saved or snapshotted.
	Truncated bool `json:"truncated"`
	// raw is the file body exactly as read, kept for history snapshots so a
	// legacy-encoded file is versioned and restored byte-for-byte. It never
	// leaves the package as JSON.
	raw []byte
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
	// Search returns the entries under one directory whose name contains query
	// (case-insensitive, name-only). Any team member may read it; the agent
	// bounds and confines the walk.
	Search(ctx context.Context, serverID uuid.UUID, root, path, query string, recursive bool) (SearchListing, error)
	Content(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error)
	// History lists one file's version snapshots, newest first. Owner/admin
	// only: a snapshot holds a file body. It reads the control plane's store,
	// so a deleted or renamed file's history is still listable.
	History(ctx context.Context, serverID uuid.UUID, root, path string) ([]Version, error)
	// HistoryVersion reads one snapshot body, scoped to (server, root, path).
	HistoryVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (VersionBody, error)
	// RestoreVersion snapshots the current body, then writes the snapshot back
	// through the conflict-checked write path. A dirty/current file must still
	// match its read etag or the restore answers 409.
	RestoreVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (WriteResult, error)
	// DeleteVersion permanently removes one snapshot; the live file is
	// untouched.
	DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) error
	// DirectorySize returns one folder's recursive size (JUS-162). It is a read:
	// any team member may call it, like List and Stat.
	DirectorySize(ctx context.Context, serverID uuid.UUID, root, path string) (FolderSize, error)
	// IssueDownloadTicket authorizes an owner/admin read of (root, path) and
	// returns a single-use, short-lived ticket for the unauthenticated
	// download route (a native browser download cannot send the bearer token).
	IssueDownloadTicket(ctx context.Context, serverID uuid.UUID, root, path string) (string, error)
	Download(ctx context.Context, serverID uuid.UUID, ticket string) (*Download, error)

	// Write creates or replaces one file. The caller declares the exact body
	// size; body must yield exactly that many bytes, or the call fails without
	// committing anything (a truncated upload never replaces the target).
	Write(ctx context.Context, serverID uuid.UUID, req WriteRequest, body io.Reader) (WriteResult, error)
	// AuthorizeWrite checks the caller may mutate the server before a streamed
	// upload begins, so a forbidden caller is refused once (403) instead of
	// once per file.
	AuthorizeWrite(ctx context.Context, serverID uuid.UUID) error
	// Mkdir creates one directory (exclusive: an existing name is a conflict).
	Mkdir(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error)
	// Create creates an empty file exclusively (an existing name is a conflict).
	Create(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error)
	// Rename renames or moves one entry within a single root.
	Rename(ctx context.Context, serverID uuid.UUID, root, source, destination string, overwrite bool) (Entry, error)
	// Chmod changes one entry's permission bits (owner/admin only). Only the
	// low 0o777 bits are accepted; setuid/setgid/sticky are refused.
	Chmod(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error)
	// Copy copies one entry within a single root and returns the new entry.
	Copy(ctx context.Context, serverID uuid.UUID, root, source, destination string, overwrite bool) (Entry, error)
	// Delete removes one entry: by default into the node's recycle bin, or
	// permanently when permanent is set.
	Delete(ctx context.Context, serverID uuid.UUID, root, path string, recursive, permanent bool) error
	// Compress builds one archive from sources inside a single root and returns
	// the created archive's entry.
	Compress(ctx context.Context, serverID uuid.UUID, req CompressRequest) (Entry, error)
	// Extract unpacks one archive into a destination directory inside a single
	// root.
	Extract(ctx context.Context, serverID uuid.UUID, req ExtractRequest) error

	// Trash lists a root's recycle bin, newest first. Any team member may read
	// it, like List. Truncated reports a capped listing.
	Trash(ctx context.Context, serverID uuid.UUID, root string) (TrashListing, error)
	// Restore moves one bin entry back to its recorded original path (or an
	// explicit destination); an existing target is a conflict unless overwrite
	// is set.
	Restore(ctx context.Context, serverID uuid.UUID, root, id, destination string, overwrite bool) (Entry, error)
	// Purge permanently removes one bin entry.
	Purge(ctx context.Context, serverID uuid.UUID, root, id string) error
	// EmptyTrash permanently removes every entry in a root's bin, reporting how
	// many were removed.
	EmptyTrash(ctx context.Context, serverID uuid.UUID, root string) (uint64, error)
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
	// MaxUploadBytes overrides the request-body ceiling for writes and uploads
	// (413 above it); 0 keeps DefaultMaxUploadBytes.
	MaxUploadBytes int64
	// HistoryMaxVersions overrides the per-file snapshot count; 0 keeps
	// HistoryMaxVersionsEnv or DefaultHistoryMaxVersions.
	HistoryMaxVersions int
	// HistoryMaxBytes overrides the per-file snapshot bytes; 0 keeps
	// HistoryMaxBytesEnv or DefaultHistoryMaxBytes.
	HistoryMaxBytes int64
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
	maxUploadBytes  int64
	// history is the optional snapshot store; nil skips snapshots and answers
	// ErrHistoryUnavailable on the history routes.
	history            HistoryStore
	historyMaxVersions int
	historyMaxBytes    int64
	tickets            *ticketStore
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
	maxUpload := cfg.MaxUploadBytes
	if maxUpload <= 0 {
		maxUpload = DefaultMaxUploadBytes
	}
	ttl := cfg.TicketTTL
	if ttl <= 0 {
		ttl = defaultTicketTTL
	}
	historyVersions := cfg.HistoryMaxVersions
	if historyVersions <= 0 {
		historyVersions = historyMaxVersionsFromEnv()
	}
	if historyVersions <= 0 {
		historyVersions = DefaultHistoryMaxVersions
	}
	historyBytes := cfg.HistoryMaxBytes
	if historyBytes <= 0 {
		historyBytes = historyMaxBytesFromEnv()
	}
	if historyBytes <= 0 {
		historyBytes = DefaultHistoryMaxBytes
	}
	return &Service{
		registry:           cfg.Registry,
		dial:               cfg.Dial,
		logger:             logger,
		rpcTimeout:         rpcTimeout,
		transferTimeout:    transferTimeout,
		maxContentBytes:    maxContent,
		maxUploadBytes:     maxUpload,
		historyMaxVersions: historyVersions,
		historyMaxBytes:    historyBytes,
		tickets:            newTicketStore(ttl, cfg.Now),
	}
}

// NewDefaultService builds the production Service for the HTTP wiring. It
// returns nil when registry is nil so callers can skip mounting the routes.
// logger is the control plane's configured logger; nil falls back to
// slog.Default, so the file audit trail is written through the same logger as
// the rest of the server instead of the process default.
func NewDefaultService(registry Registry, logger *slog.Logger) *Service {
	if registry == nil {
		return nil
	}
	return NewService(Config{Registry: registry, Logger: logger, MaxUploadBytes: maxUploadFromEnv()})
}

// SetDial installs (or replaces) the agent dialer. It is the merge-time seam
// for the mTLS dial.
func (s *Service) SetDial(dial DialFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dial = dial
}

// SetHistoryStore installs (or replaces) the version-history store. The
// control plane wires this from its shared store; a nil store leaves snapshots
// off and the history routes answering 503.
func (s *Service) SetHistoryStore(history HistoryStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = history
}

// historyStore reads the history seam under the same lock as the dialer.
func (s *Service) historyStore() HistoryStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.history
}

// MaxUploadBytes reports the write/upload body ceiling, so the HTTP layer can
// bound a request body to match the service.
func (s *Service) MaxUploadBytes() int64 {
	return s.maxUploadBytes
}

// TransferTimeout reports the budget for a long transfer, so the HTTP layer can
// extend the connection read deadline to match it.
func (s *Service) TransferTimeout() time.Duration {
	return s.transferTimeout
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
	if err := validatePathArg("path", path); err != nil {
		return Listing{}, err
	}
	if err := validateFileArg("cursor", cursor); err != nil {
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
	if err := validatePathArg("path", path); err != nil {
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
// editor ceiling answers a read-only tail (the last TailLines lines within
// MaxContentBytes bytes) flagged Truncated, with Size reporting the whole file;
// saving one answers ErrTooLarge. A genuinely binary body answers ErrNotEditable
// (415). Both are download-only in the UI. A legacy body (GBK/GB2312/BIG5) is
// decoded to UTF-8 and reported through Encoding; the tail is cut at a line
// start, so a multi-byte character is never split.
func (s *Service) Content(ctx context.Context, serverID uuid.UUID, root, path string) (content Content, err error) {
	defer func() { s.audit(ctx, serverID, "read_content", root, path, err) }()
	if !Enabled() {
		return Content{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Content{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return Content{}, err
	}
	return s.readForEditor(ctx, serverID, root, path)
}

// readForEditor reads a body for the editor, choosing a whole-file read or the
// tail of an over-limit file from one stat.
func (s *Service) readForEditor(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error) {
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
		return s.readTail(ctx, client, root, path, info)
	}
	return s.readFull(ctx, client, root, path, info)
}

// readContent reads one whole file body without validation or audit, for
// callers that already checked the path (Content, the pre-save snapshot read
// and the pre-restore snapshot). It keeps the ErrTooLarge refusal on purpose:
// history must never snapshot a truncated view of a file.
func (s *Service) readContent(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error) {
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
	return s.readFull(ctx, client, root, path, info)
}

// readFull streams a whole file body from its start, failing with ErrTooLarge
// if the file grew past the ceiling between the Stat and the read.
func (s *Service) readFull(ctx context.Context, client FileClient, root, path string, info *agentv1.FileEntry) (Content, error) {
	body, err := s.streamBody(ctx, client, &agentv1.ReadRequest{Root: root, Path: path}, s.maxContentBytes)
	if err != nil {
		return Content{}, err
	}
	return newContent(info, body, false)
}

// readTail streams the last maxContentBytes bytes of an over-limit file and
// trims them to whole lines. Shortening the requested range to the ceiling
// means the whole tail fits the editor's own budget, and starting it at a line
// start keeps every multi-byte character intact. An agent that predates the
// range fields streams from the start instead, so the read overruns the ceiling
// and answers ErrTooLarge: the tab falls back to the download-only state.
func (s *Service) readTail(ctx context.Context, client FileClient, root, path string, info *agentv1.FileEntry) (Content, error) {
	offset := info.GetSize() - s.maxContentBytes
	body, err := s.streamBody(ctx, client, &agentv1.ReadRequest{
		Root:   root,
		Path:   path,
		Offset: offset,
		Limit:  s.maxContentBytes,
	}, s.maxContentBytes)
	if err != nil {
		return Content{}, err
	}
	if len(body) == 0 && info.GetSize() > 0 {
		// The file shrank or was rotated between the Stat and the read, so the
		// window starts past EOF. Report a conflict (the UI offers a reload)
		// rather than an empty "tail" with a stale size.
		return Content{}, ErrConflict
	}
	return newContent(info, tailBytes(body), true)
}

// streamBody opens one agent read and collects at most maxBytes.
func (s *Service) streamBody(ctx context.Context, client FileClient, req *agentv1.ReadRequest, maxBytes int64) ([]byte, error) {
	stream, err := client.Read(ctx, req)
	if err != nil {
		return nil, mapRPCError(err)
	}
	return readAllLimited(stream, maxBytes)
}

// tailBytes trims a raw tail window to whole lines: it drops the partial first
// line (the window starts mid-file) and keeps at most the last TailLines lines.
// A window whose only newline is its last byte is one over-long line, so that
// line is shown (minus a leading multi-byte fragment) instead of an empty body.
func tailBytes(window []byte) []byte {
	if i := bytes.IndexByte(window, '\n'); i >= 0 {
		if rest := window[i+1:]; len(rest) > 0 {
			window = rest
		} else {
			return trimContinuation(window[:i])
		}
	} else {
		return trimContinuation(window)
	}
	// A trailing newline terminates the last line instead of starting an empty
	// one, so it is not counted.
	end := len(window)
	if end > 0 && window[end-1] == '\n' {
		end--
	}
	lines := 0
	for i := end - 1; i >= 0; i-- {
		if window[i] != '\n' {
			continue
		}
		lines++
		if lines == TailLines {
			return window[i+1:]
		}
	}
	return window
}

// trimContinuation drops a leading UTF-8 continuation fragment, so a window cut
// through a multi-byte character still decodes.
func trimContinuation(window []byte) []byte {
	for i := 0; i < 3 && len(window) > 0 && window[0]&0xC0 == 0x80; i++ {
		window = window[1:]
	}
	return window
}

// newContent decodes one editor body and pairs it with the stat it belongs to.
// truncated marks a tail view, whose Size is the whole file rather than the
// returned bytes. A body the detector refuses (binary) answers ErrNotEditable.
func newContent(info *agentv1.FileEntry, body []byte, truncated bool) (Content, error) {
	encodingName, text, err := detectEncoding(body)
	if err != nil {
		return Content{}, err
	}
	sum := sha256.Sum256(body)
	content := Content{
		Content:   text,
		Encoding:  encodingName,
		Etag:      info.GetEtag(),
		SHA256:    hex.EncodeToString(sum[:]),
		Size:      int64(len(body)),
		Mode:      info.GetMode(),
		raw:       body,
		Truncated: truncated,
	}
	if truncated {
		content.Size = info.GetSize()
	}
	return content, nil
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
	if err := validatePathArg("path", path); err != nil {
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
// ticket for a different server, answers ErrNotFound; a wrong-server request
// leaves the ticket intact rather than burning it.
func (s *Service) Download(ctx context.Context, serverID uuid.UUID, ticketID string) (download *Download, err error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	t, ok := s.tickets.consumeFor(ticketID, serverID)
	if !ok {
		return nil, ErrNotFound
	}
	defer func() { s.auditActor(t.UserID, t.TeamID, serverID, "download", t.Root, t.Path, err) }()
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
// It follows the rest of the node surface (AuthorizeOptionalTeam, like
// ListServers): a team node belongs to its team, so a foreign server answers
// ErrNotFound; write requires an owner/admin role and the remaining roles read.
// A legacy team-less node (team_id NULL, e.g. the all-in-one localhost node)
// stays available to anyone who passes the router's platform-operator gate
// (RequirePlatformAdmin in internal/server), which is what protects the host
// filesystem: the files surface is never mounted for plain team members. A
// request without a team scope (teams not wired, a non-HTTP caller) keeps the
// pre-teams compatibility path.
func (s *Service) resolve(ctx context.Context, serverID uuid.UUID, write bool) (*servers.Server, error) {
	server, err := s.server(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, write); err != nil {
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
		// Return the (canceled) ctx, never nil: a deferred audit still needs the
		// request's team/user scope, which survives cancellation.
		return ctx, nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return ctx, nil, nil, err
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

// validateRoot rejects an empty root name and any value the gRPC layer cannot
// carry, before it reaches the agent.
func validateRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("%w: root is required", ErrValidation)
	}
	return validatePathArg("root", root)
}

// validatePathArg validates a path-like argument (path/source/destination/root)
// and additionally rejects a ".." segment. The agent's fsroot confines every
// open, so this is not the security boundary; it is a cheap pre-dial check that
// answers 400 for an obviously traversal-shaped path instead of a round trip.
func validatePathArg(name, value string) error {
	if err := validateFileArg(name, value); err != nil {
		return err
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return fmt.Errorf("%w: %s must not contain a parent segment", ErrValidation, name)
		}
	}
	return nil
}

// validateFileArg rejects a root/path/cursor value the agent gRPC layer cannot
// carry: invalid UTF-8 (proto.Marshal fails on it, which would otherwise
// surface as a 500), an embedded NUL byte, or a value longer than any real path
// (maxFileArgBytes). It runs before the dial, so a malformed request is a 400
// and never reaches the agent.
func validateFileArg(name, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrValidation, name)
	}
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%w: %s contains a NUL byte", ErrValidation, name)
	}
	if len(value) > maxFileArgBytes {
		return fmt.Errorf("%w: %s is too long", ErrValidation, name)
	}
	return nil
}

// readAllLimited streams a body, failing with ErrTooLarge once it exceeds
// limit bytes. A file that grows past the ceiling between the Stat and the read
// is still refused.
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
		Owner:         info.GetOwner(),
		Group:         info.GetGroup(),
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
		// A corrupt or truncated archive body (a bad zip/tar/gzip read, a
		// truncated member, a member name the format forbids) is an invalid
		// archive, not a malformed request; it gets its own 400 copy.
		if isInvalidArchiveError(status.Convert(err).Message()) {
			return fmt.Errorf("%w: %v", ErrInvalidArchive, err)
		}
		return fmt.Errorf("%w: %v", ErrValidation, err)
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %v", ErrForbidden, err)
	case codes.FailedPrecondition:
		// The agent's FailedPrecondition is normally an etag/type conflict. A
		// refused archive member (a link, device, FIFO or unsupported type) is
		// the one FailedPrecondition that is not a conflict, so it gets its own
		// message instead of surfacing as "conflict".
		if strings.Contains(status.Convert(err).Message(), "archive entry") {
			return fmt.Errorf("%w: %v", ErrArchiveEntry, err)
		}
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case codes.AlreadyExists, codes.Aborted:
		// AlreadyExists is an exclusive create or no-overwrite rename/copy
		// hitting an existing name, and Aborted a target that changed during
		// the upload. Both are 409.
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case codes.ResourceExhausted:
		return classifyResourceExhausted(err)
	case codes.Unimplemented:
		return fmt.Errorf("%w: %v", ErrAgentOutdated, err)
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	default:
		return err
	}
}

// classifyResourceExhausted splits the agent's single ResourceExhausted code
// into the distinct conditions the proto message prefixes already distinguish
// (agent/files_write.go, agent/files_archive.go), so a caller is not told "the
// node is busy" when the real problem is a full disk or an archive over its
// limits. Free-space checks run first, because an out-of-space message embeds
// the path being written and a path may legitimately contain "archive":
//
//   - low free space or an exhausted quota is ErrInsufficientStorage (507);
//   - an archive over its entry/byte/ratio/depth limits is ErrArchiveLimit
//     (413, with an archive-specific message);
//   - a body over the node's upload limit is ErrTooLarge (413);
//   - the per-node transfer semaphore being full is ErrBusy (429).
//
// An unrecognised message keeps the historical ErrBusy mapping.
func classifyResourceExhausted(err error) error {
	message := strings.ToLower(status.Convert(err).Message())
	switch {
	case strings.Contains(message, "low on free space"),
		strings.Contains(message, "do not fit"),
		strings.Contains(message, "quota"),
		strings.Contains(message, "no space left"),
		strings.Contains(message, "disk full"):
		return fmt.Errorf("%w: %v", ErrInsufficientStorage, err)
	case strings.Contains(message, "archive has more than"),
		strings.Contains(message, "archive expands"),
		strings.Contains(message, "archive nesting"),
		strings.Contains(message, "nests too deeply"),
		strings.Contains(message, "archive entry has a negative size"),
		strings.Contains(message, "archive is larger than"):
		// An archive limit (entry count, uncompressed bytes, compression ratio,
		// nesting depth, archive size) is its own condition.
		return fmt.Errorf("%w: %v", ErrArchiveLimit, err)
	case strings.Contains(message, "upload limit"), strings.Contains(message, "too large"):
		return fmt.Errorf("%w: %v", ErrTooLarge, err)
	default:
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
}

// isInvalidArchiveError reports whether an agent InvalidArgument message
// describes a corrupt or unsupported archive body rather than a malformed
// request, so the API answers a clear invalid-archive message instead of the
// generic "invalid request". The phrases mirror the agent's archive errors
// (agent/files_archive.go); a wording change there must be reflected here.
func isInvalidArchiveError(message string) bool {
	message = strings.ToLower(message)
	for _, phrase := range []string{
		"reading zip",
		"reading tar",
		"reading gzip",
		"opening archive entry",
		"archive entry",
		"unsupported archive format",
		"is not a .zip",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// auditResult renders a service error for the audit log as a small stable
// token, so the log records the outcome without echoing raw agent text.
func auditResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrValidation):
		return "invalid"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrArchiveLimit):
		return "archive_limit"
	case errors.Is(err, ErrArchiveEntry):
		return "archive_entry"
	case errors.Is(err, ErrInvalidArchive):
		return "invalid_archive"
	case errors.Is(err, ErrNotEditable):
		return "not_editable"
	case errors.Is(err, ErrUnrepresentable):
		return "unrepresentable"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrInsufficientStorage):
		return "insufficient_storage"
	case errors.Is(err, ErrAgentOutdated):
		return "agent_outdated"
	case errors.Is(err, ErrAgentUnavailable):
		return "agent_unavailable"
	case errors.Is(err, ErrDisabled):
		return "disabled"
	default:
		return "error"
	}
}

// audit logs one file operation for the actor in ctx. It records the actor,
// target and outcome but never the file body, a token or the download ticket:
// file bodies hold secrets, so only the (root, relpath) addressing them is
// recorded. extra appends op-specific fields (e.g. a rename destination).
func (s *Service) audit(ctx context.Context, serverID uuid.UUID, op, root, relpath string, err error, extra ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	scope := teams.ScopeFor(ctx, uuid.Nil)
	s.auditActor(scope.UserID, scope.TeamID, serverID, op, root, relpath, err, extra...)
}

// auditActor is audit for a caller whose actor is not in ctx (the download
// route carries its actor in the ticket, not a team scope).
func (s *Service) auditActor(userID, teamID, serverID uuid.UUID, op, root, relpath string, err error, extra ...any) {
	args := []any{
		"op", op,
		"result", auditResult(err),
		"actor_user_id", scopeID(userID),
		"actor_team_id", scopeID(teamID),
		"server_id", serverID.String(),
		"root", root,
		"relpath", relpath,
	}
	s.logger.Info("files: audit", append(args, extra...)...)
}

// scopeID renders a scope UUID for audit logs, leaving it empty when the caller
// had no team/user scope, so logs never show the zero UUID as an actor.
func scopeID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
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

// consumeFor removes and returns the ticket for token, but only when it is
// bound to serverID. It reports false when the token is unknown, expired or
// already consumed (so a ticket is single-use) and also when it belongs to a
// different server — in that last case the ticket is left in the store, not
// burned, so a request for the wrong server cannot invalidate a valid ticket.
func (s *ticketStore) consumeFor(token string, serverID uuid.UUID) (ticket, bool) {
	if token == "" {
		return ticket{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	t, ok := s.tickets[token]
	if !ok || t.ServerID != serverID {
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
