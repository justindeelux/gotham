package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TransferAuthorizer is the control plane's both-node policy check. The HTTP
// mount authorizes the source server for the request URL; this checks that the
// same caller may also reach the destination server. File routes are
// operator-only today, so the canonical check resolves the owner role on every
// request instead of trusting the source URL.
type TransferAuthorizer interface {
	AuthorizeTransfer(ctx context.Context, sourceID, destinationID uuid.UUID) error
}

// TransferAuthorizerFunc adapts a function to TransferAuthorizer.
type TransferAuthorizerFunc func(ctx context.Context, sourceID, destinationID uuid.UUID) error

func (f TransferAuthorizerFunc) AuthorizeTransfer(ctx context.Context, sourceID, destinationID uuid.UUID) error {
	return f(ctx, sourceID, destinationID)
}

const (
	TransferModeDirect = "direct"
	TransferModeBundle = "bundle"

	TransferPhasePreparing    = "preparing"
	TransferPhaseTransferring = "transferring"
	TransferPhaseExtracting   = "extracting"
	TransferPhasePublishing   = "publishing"
	TransferPhaseCleaningUp   = "cleaning_up"

	TransferStatusCopied    = "copied"
	TransferStatusConflict  = "conflict"
	TransferStatusSkipped   = "skipped"
	TransferStatusFailed    = "failed"
	TransferStatusCancelled = "cancelled"
	TransferStatusUncertain = "uncertain"

	TransferEventPhase   = "phase"
	TransferEventResult  = "result"
	TransferEventSummary = "summary"
)

const (
	// transferChunkBuffer bounds how many artifact chunks may wait between the
	// source and destination streams; memory is bounded by chunk size, not the
	// bundle size.
	transferChunkBuffer = 4

	// transferReservedDir and transferTrashDir are agent-internal trees that a
	// transfer selection must never read.
	transferReservedDir = ".gotham-transfer"
	transferTrashDir    = ".gotham-trash"

	transferCleanupTimeout = 15 * time.Second
	transferSummaryTimeout = 5 * time.Second
	transferProgress       = time.Second
)

// TransferDestination names the receiving node, root and directory.
type TransferDestination struct {
	ServerID uuid.UUID `json:"server_id"`
	Root     string    `json:"root"`
	Path     string    `json:"path"`
}

// TransferRequest selects direct children of one source directory and copies
// them into a destination directory on another node.
type TransferRequest struct {
	SourceRoot      string              `json:"source_root"`
	SourceDirectory string              `json:"source_directory"`
	Sources         []string            `json:"sources"`
	Destination     TransferDestination `json:"destination"`
}

// TransferBounds are the effective limits: the minimum the two nodes negotiate.
type TransferBounds struct {
	MaxBundleBytes   uint64 `json:"max_bundle_bytes"`
	MaxExpandedBytes uint64 `json:"max_expanded_bytes"`
	MaxEntries       uint64 `json:"max_entries"`
	MaxSources       uint32 `json:"max_sources"`
	MaxConcurrency   uint32 `json:"max_concurrency"`
	BundleAvailable  bool   `json:"bundle_available"`
}

// TransferPlanSource is one selected top-level entry.
type TransferPlanSource struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	Conflict bool   `json:"conflict"`
}

// TransferPlan is the read-only preflight result. It never creates staging.
type TransferPlan struct {
	Mode           string               `json:"mode"`
	Bounds         TransferBounds       `json:"bounds"`
	Sources        []TransferPlanSource `json:"sources"`
	Conflicts      []string             `json:"conflicts"`
	Unsupported    []TransferPlanSource `json:"unsupported"`
	AvailableBytes *uint64              `json:"available_bytes,omitempty"`
	Fits           *bool                `json:"fits,omitempty"`
}

// TransferEvent is one NDJSON line. Phase and result events carry actual bytes;
// total_bytes is present only when the phase knows the total. Exactly one
// summary event terminates a delivered stream.
type TransferEvent struct {
	Type       string           `json:"type"`
	Phase      string           `json:"phase,omitempty"`
	Name       string           `json:"name,omitempty"`
	Status     string           `json:"status,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	Bytes      int64            `json:"bytes,omitempty"`
	TotalBytes *int64           `json:"total_bytes,omitempty"`
	Summary    *TransferSummary `json:"summary,omitempty"`
}

// TransferOutcome is one top-level entry's terminal status.
type TransferOutcome struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// TransferSkipped is one bounded sample of an entry a bundle left out.
type TransferSkipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// TransferSummary is the terminal event's payload.
type TransferSummary struct {
	TransferID      string            `json:"transfer_id"`
	Mode            string            `json:"mode"`
	Status          string            `json:"status"`
	Reason          string            `json:"reason,omitempty"`
	Copied          int               `json:"copied"`
	Conflicts       int               `json:"conflicts"`
	Skipped         int               `json:"skipped"`
	Failed          int               `json:"failed"`
	Cancelled       int               `json:"cancelled"`
	Uncertain       int               `json:"uncertain"`
	Entries         []TransferOutcome `json:"entries"`
	SkippedCount    int               `json:"skipped_count"`
	SkippedSamples  []TransferSkipped `json:"skipped_samples,omitempty"`
	CleanupFailures int               `json:"cleanup_failures,omitempty"`
}

// TransferRun streams phase, result and one terminal summary event. The channel
// closes when the run ends and its staging has been discarded.
type TransferRun struct {
	ID     string
	Mode   string
	Events <-chan TransferEvent
}

type transferInput struct {
	sourceID      uuid.UUID
	destinationID uuid.UUID
	sourceRoot    string
	sourceDir     string
	sources       []string
	destRoot      string
	destDir       string
}

func (in transferInput) sourcePath(name string) string      { return joinRel(in.sourceDir, name) }
func (in transferInput) destinationPath(name string) string { return joinRel(in.destDir, name) }

type transferCaps struct {
	available bool
	bounds    TransferBounds
}

// PlanTransfer validates and inspects both nodes without creating anything.
func (s *Service) PlanTransfer(ctx context.Context, sourceID uuid.UUID, req TransferRequest) (TransferPlan, error) {
	if !Enabled() {
		return TransferPlan{}, ErrDisabled
	}
	in, err := validateTransferRequest(sourceID, req)
	if err != nil {
		return TransferPlan{}, err
	}
	plan := TransferPlan{Conflicts: []string{}, Sources: []TransferPlanSource{}, Unsupported: []TransferPlanSource{}}
	err = s.withTransferClient(ctx, sourceID, func(rpcCtx context.Context, src FileClient) error {
		return s.withTransferClient(ctx, in.destinationID, func(dstCtx context.Context, dst FileClient) error {
			pre, preflightErr := s.preflightTransfer(rpcCtx, dstCtx, src, dst, in)
			if preflightErr != nil {
				return preflightErr
			}
			plan.Mode = pre.mode
			plan.Bounds = pre.bounds
			plan.Sources = pre.sources
			plan.Conflicts = pre.conflicts
			plan.Unsupported = pre.unsupported
			plan.AvailableBytes = pre.freeBytes
			if pre.mode == TransferModeDirect && pre.freeBytes != nil && len(pre.sources) > 0 {
				fits := *pre.freeBytes >= uint64(max(pre.sources[0].Size, 0))
				plan.Fits = &fits
			}
			return nil
		})
	})
	if err != nil {
		return TransferPlan{}, err
	}
	return plan, nil
}

// Transfer revalidates the request, repeats preflight and returns a stream that
// carries the transfer to completion. Errors before the stream exists are
// returned directly; everything after that is a typed result or summary event.
func (s *Service) Transfer(ctx context.Context, sourceID uuid.UUID, req TransferRequest) (*TransferRun, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	in, err := validateTransferRequest(sourceID, req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.transferTimeout)
	src, err := s.openTransferClient(ctx, sourceID)
	if err != nil {
		cancel()
		return nil, err
	}
	dst, err := s.openTransferClient(ctx, in.destinationID)
	if err != nil {
		s.closeClient(src)
		cancel()
		return nil, err
	}
	pre, err := s.preflightTransfer(ctx, ctx, src, dst, in)
	if err != nil {
		s.closeClient(src)
		s.closeClient(dst)
		cancel()
		return nil, err
	}
	events := make(chan TransferEvent, 16)
	run := &TransferRun{ID: newTransferID(), Mode: pre.mode, Events: events}
	go func() {
		defer close(events)
		defer cancel()
		defer s.closeClient(src)
		defer s.closeClient(dst)
		s.executeTransfer(ctx, events, src, dst, in, pre, run.ID)
	}()
	return run, nil
}

func newTransferID() string {
	return uuid.NewString()
}

func (s *Service) withTransferClient(ctx context.Context, serverID uuid.UUID, fn func(context.Context, FileClient) error) error {
	rpcCtx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)
	return fn(rpcCtx, client)
}

func (s *Service) openTransferClient(ctx context.Context, serverID uuid.UUID) (FileClient, error) {
	server, err := s.server(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return s.dialClient(ctx, server)
}

func validateTransferRequest(sourceID uuid.UUID, req TransferRequest) (transferInput, error) {
	in := transferInput{
		sourceID:      sourceID,
		destinationID: req.Destination.ServerID,
		sourceRoot:    req.SourceRoot,
		sourceDir:     req.SourceDirectory,
		sources:       append([]string(nil), req.Sources...),
		destRoot:      req.Destination.Root,
		destDir:       req.Destination.Path,
	}
	if sourceID == uuid.Nil {
		return transferInput{}, fmt.Errorf("%w: source server id is required", ErrValidation)
	}
	if in.destinationID == uuid.Nil {
		return transferInput{}, fmt.Errorf("%w: destination.server_id is required", ErrValidation)
	}
	if in.destinationID == sourceID {
		return transferInput{}, fmt.Errorf("%w: source and destination servers are the same", ErrValidation)
	}
	if err := validateRoot(in.sourceRoot); err != nil {
		return transferInput{}, err
	}
	if err := validateRoot(in.destRoot); err != nil {
		return transferInput{}, err
	}
	if strings.TrimSpace(in.destDir) == "" {
		in.destDir = ""
	}
	if err := validatePathArg("source_directory", in.sourceDir); err != nil {
		return transferInput{}, err
	}
	if err := validatePathArg("destination.path", in.destDir); err != nil {
		return transferInput{}, err
	}
	if len(in.sources) == 0 {
		return transferInput{}, fmt.Errorf("%w: at least one source is required", ErrValidation)
	}
	if len(in.sources) > maxArchiveSources {
		return transferInput{}, fmt.Errorf("%w: at most %d sources are accepted", ErrValidation, maxArchiveSources)
	}
	seen := make(map[string]struct{}, len(in.sources))
	for _, name := range in.sources {
		if err := validateTransferName(name); err != nil {
			return transferInput{}, err
		}
		if _, ok := seen[name]; ok {
			return transferInput{}, fmt.Errorf("%w: duplicate source %q", ErrValidation, name)
		}
		seen[name] = struct{}{}
	}
	return in, nil
}

func validateTransferName(name string) error {
	if !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("%w: source name is not valid text", ErrValidation)
	}
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%w: source name is required", ErrValidation)
	}
	if strings.ContainsAny(name, `/\`) || path.Base(name) != name {
		return fmt.Errorf("%w: only direct children of the source directory can be selected", ErrValidation)
	}
	if name == transferReservedDir || name == transferTrashDir {
		return fmt.Errorf("%w: %q is reserved", ErrValidation, name)
	}
	if err := validateFileArg("source", name); err != nil {
		return err
	}
	return nil
}

type transferPreflight struct {
	mode        string
	bounds      TransferBounds
	sources     []TransferPlanSource
	conflicts   []string
	unsupported []TransferPlanSource
	entries     map[string]Entry
	freeBytes   *uint64
}

func (s *Service) preflightTransfer(ctx, dstCtx context.Context, src, dst FileClient, in transferInput) (*transferPreflight, error) {
	srcCaps, err := transferCapabilities(ctx, src)
	if err != nil {
		return nil, err
	}
	dstCaps, err := transferCapabilities(dstCtx, dst)
	if err != nil {
		return nil, err
	}
	srcRoots, err := listTransferRoots(ctx, src)
	if err != nil {
		return nil, err
	}
	dstRoots, err := listTransferRoots(dstCtx, dst)
	if err != nil {
		return nil, err
	}
	if _, ok := findRoot(srcRoots, in.sourceRoot); !ok {
		return nil, fmt.Errorf("%w: unknown source root %q", ErrValidation, in.sourceRoot)
	}
	dstRoot, ok := findRoot(dstRoots, in.destRoot)
	if !ok {
		return nil, fmt.Errorf("%w: unknown destination root %q", ErrValidation, in.destRoot)
	}
	if !dstRoot.Writable {
		return nil, fmt.Errorf("%w: destination root %q is read-only", ErrForbidden, in.destRoot)
	}
	sourceDir, err := statTransferEntry(ctx, src, in.sourceRoot, in.sourceDir)
	if err != nil {
		return nil, err
	}
	if sourceDir.Kind != "directory" {
		return nil, fmt.Errorf("%w: source_directory is not a directory", ErrValidation)
	}
	destinationDir, err := statTransferEntry(dstCtx, dst, in.destRoot, in.destDir)
	if err != nil {
		return nil, err
	}
	if destinationDir.Kind != "directory" {
		return nil, fmt.Errorf("%w: destination.path is not a directory", ErrValidation)
	}
	pre := &transferPreflight{
		bounds:      s.negotiatedTransferBounds(srcCaps, dstCaps),
		conflicts:   []string{},
		unsupported: []TransferPlanSource{},
		sources:     make([]TransferPlanSource, 0, len(in.sources)),
		entries:     make(map[string]Entry, len(in.sources)),
		freeBytes:   &dstRoot.FreeBytes,
	}
	if srcCaps.available && srcCaps.bounds.MaxSources > 0 && uint32(len(in.sources)) > srcCaps.bounds.MaxSources {
		return nil, fmt.Errorf("%w: this node accepts at most %d sources", ErrValidation, srcCaps.bounds.MaxSources)
	}
	if dstCaps.available && dstCaps.bounds.MaxSources > 0 && uint32(len(in.sources)) > dstCaps.bounds.MaxSources {
		return nil, fmt.Errorf("%w: the destination accepts at most %d sources", ErrValidation, dstCaps.bounds.MaxSources)
	}

	for _, name := range in.sources {
		entry, statErr := statTransferEntry(ctx, src, in.sourceRoot, in.sourcePath(name))
		if statErr != nil {
			return nil, statErr
		}
		item := TransferPlanSource{Name: name, Kind: entry.Kind, Size: entry.Size}
		if entry.Kind != "file" && entry.Kind != "directory" {
			item.Conflict = false
			pre.unsupported = append(pre.unsupported, item)
			pre.sources = append(pre.sources, item)
			continue
		}
		if _, destErr := statTransferEntry(dstCtx, dst, in.destRoot, in.destinationPath(name)); destErr == nil {
			item.Conflict = true
			pre.conflicts = append(pre.conflicts, name)
		} else if !errors.Is(destErr, ErrNotFound) {
			return nil, destErr
		}
		pre.entries[name] = entry
		pre.sources = append(pre.sources, item)
	}

	if len(in.sources) == 1 && len(pre.unsupported) == 0 && pre.sources[0].Kind == "file" {
		pre.mode = TransferModeDirect
		if pre.sources[0].Size > s.maxUploadBytes {
			return nil, ErrTooLarge
		}
		return pre, nil
	}
	pre.mode = TransferModeBundle
	if !srcCaps.available || !dstCaps.available {
		return nil, fmt.Errorf("%w: both nodes must support transfer bundles", ErrAgentOutdated)
	}
	return pre, nil
}

func (s *Service) negotiatedTransferBounds(src, dst transferCaps) TransferBounds {
	bounds := TransferBounds{
		MaxBundleBytes:   uint64(max(s.maxUploadBytes, 0)),
		MaxExpandedBytes: uint64(DefaultMaxUploadBytes) * 4,
		MaxEntries:       100_000,
		MaxSources:       maxArchiveSources,
	}
	if src.available {
		bounds = minTransferBounds(bounds, src.bounds)
	}
	if dst.available {
		bounds = minTransferBounds(bounds, dst.bounds)
	}
	bounds.BundleAvailable = src.available && dst.available
	return bounds
}

func minTransferBounds(a, b TransferBounds) TransferBounds {
	return TransferBounds{
		MaxBundleBytes:   minPositive(a.MaxBundleBytes, b.MaxBundleBytes),
		MaxExpandedBytes: minPositive(a.MaxExpandedBytes, b.MaxExpandedBytes),
		MaxEntries:       minPositive(a.MaxEntries, b.MaxEntries),
		MaxSources:       minPositive32(a.MaxSources, b.MaxSources),
		MaxConcurrency:   minPositive32(a.MaxConcurrency, b.MaxConcurrency),
	}
}

func minPositive(a, b uint64) uint64 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case b < a:
		return b
	default:
		return a
	}
}

func minPositive32(a, b uint32) uint32 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case b < a:
		return b
	default:
		return a
	}
}

func transferCapabilities(ctx context.Context, client FileClient) (transferCaps, error) {
	response, err := client.TransferCapabilities(ctx, &agentv1.TransferCapabilitiesRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return transferCaps{available: false}, nil
		}
		return transferCaps{}, mapTransferRPCError(err)
	}
	if response == nil || !response.GetAvailable() {
		return transferCaps{available: false}, nil
	}
	return transferCaps{available: true, bounds: TransferBounds{
		MaxBundleBytes:   response.GetMaxBundleBytes(),
		MaxExpandedBytes: response.GetMaxExpandedBytes(),
		MaxEntries:       response.GetMaxEntries(),
		MaxSources:       response.GetMaxSources(),
		MaxConcurrency:   response.GetMaxConcurrency(),
	}}, nil
}

func listTransferRoots(ctx context.Context, client FileClient) ([]Root, error) {
	response, err := client.ListRoots(ctx, &agentv1.ListRootsRequest{})
	if err != nil {
		return nil, mapTransferRPCError(err)
	}
	roots := make([]Root, 0, len(response.GetRoots()))
	for _, info := range response.GetRoots() {
		roots = append(roots, newRoot(info))
	}
	return roots, nil
}

func findRoot(roots []Root, name string) (Root, bool) {
	for _, root := range roots {
		if root.Name == name {
			return root, true
		}
	}
	return Root{}, false
}

func statTransferEntry(ctx context.Context, client FileClient, root, relpath string) (Entry, error) {
	response, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: relpath})
	if err != nil {
		return Entry{}, mapTransferRPCError(err)
	}
	info := response.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func mapTransferRPCError(err error) error {
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return fmt.Errorf("%w: %v", ErrTransferTimeout, err)
	case codes.Canceled:
		return fmt.Errorf("%w: %v", ErrTransferCancelled, err)
	default:
		return mapRPCError(err)
	}
}

type transferResults struct {
	entries         []TransferOutcome
	seen            map[string]struct{}
	skippedCount    int
	skippedSamples  []TransferSkipped
	cleanupFailures int
}

func newTransferResults() *transferResults {
	return &transferResults{seen: make(map[string]struct{})}
}

func (r *transferResults) add(name, status, reason string) {
	if name == "" {
		return
	}
	if _, ok := r.seen[name]; ok {
		return
	}
	r.seen[name] = struct{}{}
	r.entries = append(r.entries, TransferOutcome{Name: name, Status: status, Reason: reason})
}

func (r *transferResults) counts() TransferSummary {
	summary := TransferSummary{Entries: r.entries, SkippedCount: r.skippedCount, SkippedSamples: r.skippedSamples, CleanupFailures: r.cleanupFailures}
	for _, entry := range r.entries {
		switch entry.Status {
		case TransferStatusCopied:
			summary.Copied++
		case TransferStatusConflict:
			summary.Conflicts++
		case TransferStatusSkipped:
			summary.Skipped++
		case TransferStatusCancelled:
			summary.Cancelled++
		case TransferStatusUncertain:
			summary.Uncertain++
		default:
			summary.Failed++
		}
	}
	return summary
}

func (r *transferResults) summary(id, mode string) TransferSummary {
	summary := r.counts()
	summary.TransferID = id
	summary.Mode = mode
	switch {
	case summary.Uncertain > 0:
		summary.Status = TransferStatusUncertain
	case summary.Cancelled > 0:
		summary.Status = TransferStatusCancelled
	case summary.Failed > 0:
		summary.Status = TransferStatusFailed
	case summary.Conflicts > 0:
		summary.Status = "partial"
	default:
		summary.Status = TransferStatusCopied
	}
	return summary
}

// finalTransferSummary derives the terminal status from the recorded entries,
// then marks an interrupted run that produced no entry of its own. The deadline
// is reported distinctly from a client cancel, because a deadline keeps the
// browser connected and can still deliver the terminal event.
func finalTransferSummary(results *transferResults, id, mode string, ctx context.Context) TransferSummary {
	summary := results.summary(id, mode)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if len(summary.Entries) == 0 && summary.Status == TransferStatusCopied {
			summary.Status = TransferStatusFailed
		}
		if summary.Status != TransferStatusCopied {
			summary.Reason = "timeout"
		}
		return summary
	}
	if ctx.Err() != nil && len(summary.Entries) == 0 && summary.Status == TransferStatusCopied {
		summary.Status = TransferStatusCancelled
	}
	return summary
}

func emitTransferEvent(ctx context.Context, events chan<- TransferEvent, event TransferEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitTransferPhase(ctx context.Context, events chan<- TransferEvent, phase string, bytes int64, total *int64) bool {
	return emitTransferEvent(ctx, events, TransferEvent{Type: TransferEventPhase, Phase: phase, Bytes: bytes, TotalBytes: total})
}

func (s *Service) executeTransfer(ctx context.Context, events chan TransferEvent, src, dst FileClient, in transferInput, pre *transferPreflight, id string) {
	results := newTransferResults()
	defer func() { s.auditTransfer(in, finalTransferSummary(results, id, pre.mode, ctx)) }()
	if pre.mode == TransferModeDirect {
		s.runDirectTransfer(ctx, events, src, dst, in, pre, results)
	} else {
		s.runBundleTransfer(ctx, events, src, dst, in, pre, results, id)
	}
	if ctx.Err() == nil {
		summary := results.summary(id, pre.mode)
		event := TransferEvent{Type: TransferEventSummary, Summary: &summary, Status: summary.Status}
		emitTransferEvent(ctx, events, event)
		return
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// A client cancel has no listener left; skip the terminal event.
		return
	}
	summary := finalTransferSummary(results, id, pre.mode, ctx)
	event := TransferEvent{Type: TransferEventSummary, Summary: &summary, Status: summary.Status}
	emitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transferSummaryTimeout)
	defer cancel()
	emitTransferEvent(emitCtx, events, event)
}

func (s *Service) runDirectTransfer(ctx context.Context, events chan TransferEvent, src, dst FileClient, in transferInput, pre *transferPreflight, results *transferResults) {
	name := in.sources[0]
	entry := pre.entries[name]
	srcPath := in.sourcePath(name)
	dstPath := in.destinationPath(name)
	if !emitTransferPhase(ctx, events, TransferPhaseTransferring, 0, &entry.Size) {
		return
	}
	if pre.sources[0].Conflict {
		results.add(name, TransferStatusConflict, "destination already exists")
		return
	}

	srcCtx, srcCancel := context.WithCancel(ctx)
	defer srcCancel()
	dstCtx, dstCancel := context.WithCancel(ctx)
	defer dstCancel()

	stream, err := src.Read(srcCtx, &agentv1.ReadRequest{Root: in.sourceRoot, Path: srcPath})
	if err != nil {
		results.add(name, transferFailureStatus(mapTransferRPCError(err)), transferFailureReason(err))
		return
	}
	write, err := dst.Write(dstCtx)
	if err != nil {
		results.add(name, transferFailureStatus(mapTransferRPCError(err)), transferFailureReason(err))
		return
	}
	header := &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{
		Root: in.destRoot, Path: dstPath, Size: entry.Size, Overwrite: false, ExpectEtag: "",
	}}}
	if err := sendWritePart(write, header); err != nil {
		results.add(name, transferFailureStatus(err), transferFailureReason(err))
		return
	}

	hasher := sha256.New()
	var sent int64
	last := time.Now()
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			dstCancel()
			results.add(name, transferFailureStatus(mapTransferRPCError(recvErr)), transferFailureReason(recvErr))
			return
		}
		data := chunk.GetData()
		if len(data) == 0 {
			continue
		}
		sent += int64(len(data))
		if sent > entry.Size {
			dstCancel()
			results.add(name, TransferStatusFailed, "source changed during the transfer")
			return
		}
		_, _ = hasher.Write(data)
		if err := sendWritePart(write, &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Data{Data: data}}); err != nil {
			srcCancel()
			results.add(name, transferFailureStatus(err), transferFailureReason(err))
			return
		}
		if time.Since(last) >= transferProgress {
			last = time.Now()
			emitTransferPhase(ctx, events, TransferPhaseTransferring, sent, &entry.Size)
		}
	}
	if sent != entry.Size {
		dstCancel()
		results.add(name, TransferStatusFailed, "source changed during the transfer")
		return
	}
	after, err := statTransferEntry(ctx, src, in.sourceRoot, srcPath)
	if err != nil {
		dstCancel()
		results.add(name, transferFailureStatus(err), transferFailureReason(err))
		return
	}
	if after.Etag != entry.Etag || after.Size != entry.Size {
		dstCancel()
		results.add(name, TransferStatusFailed, "source changed during the transfer")
		return
	}
	commit := &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Commit{Commit: &agentv1.WriteCommit{Sha256: hex.EncodeToString(hasher.Sum(nil))}}}
	if err := sendWritePart(write, commit); err != nil {
		results.add(name, transferFailureStatus(err), transferFailureReason(err))
		return
	}
	emitTransferPhase(ctx, events, TransferPhaseTransferring, sent, &entry.Size)
	response, err := write.CloseAndRecv()
	if err != nil {
		results.add(name, transferAcknowledgementStatus(err), transferFailureReason(err))
		return
	}
	if response.GetEntry() == nil {
		results.add(name, TransferStatusUncertain, "destination did not acknowledge the entry")
		return
	}
	results.add(name, TransferStatusCopied, "")
}

func (s *Service) runBundleTransfer(ctx context.Context, events chan TransferEvent, src, dst FileClient, in transferInput, pre *transferPreflight, results *transferResults, id string) {
	if !emitTransferPhase(ctx, events, TransferPhasePreparing, 0, nil) {
		return
	}
	prepared, err := src.PrepareBundle(ctx, &agentv1.PrepareBundleRequest{
		TransferId: id,
		Root:       in.sourceRoot,
		Sources:    transferSourcePaths(in),
	})
	if err != nil {
		err = mapTransferRPCError(err)
		s.logger.Warn("files: transfer prepare failed", "transfer_id", id, "error", err)
		for _, name := range in.sources {
			results.add(name, transferFailureStatus(err), transferFailureReason(err))
		}
		return
	}
	selected := make(map[string]struct{}, len(in.sources))
	for _, name := range in.sources {
		selected[name] = struct{}{}
	}
	for _, sample := range prepared.GetSkipped() {
		// Samples name nested archive paths; only a sample that names a selected
		// top-level source is a top-level outcome. Nested skips stay in
		// SkippedCount/SkippedSamples so Entries remains per-top-level.
		if _, ok := selected[sample.GetPath()]; ok {
			results.add(sample.GetPath(), TransferStatusSkipped, sample.GetReason())
		}
	}
	results.skippedCount = int(prepared.GetSkippedCount())
	for _, sample := range prepared.GetSkipped() {
		results.skippedSamples = append(results.skippedSamples, TransferSkipped{Path: sample.GetPath(), Reason: sample.GetReason()})
	}
	if err := checkBundleBounds(prepared, pre.bounds, len(in.sources)); err != nil {
		for _, name := range prepared.GetTopLevel() {
			results.add(name, TransferStatusFailed, transferFailureReason(err))
		}
		cleanupTransferArtifacts(ctx, events, s, src, dst, results, id, prepared.GetArtifact(), "")
		return
	}

	if !emitTransferPhase(ctx, events, TransferPhaseTransferring, 0, int64Pointer(int64(prepared.GetCompressedBytes()))) {
		cleanupTransferArtifacts(ctx, events, s, src, dst, results, id, prepared.GetArtifact(), "")
		return
	}
	received, err := s.relayBundle(ctx, events, src, dst, in, prepared, id)
	if err != nil {
		status := transferFailureStatus(err)
		reason := transferFailureReason(err)
		for _, name := range prepared.GetTopLevel() {
			results.add(name, status, reason)
		}
		cleanupTransferArtifacts(ctx, events, s, src, dst, results, id, prepared.GetArtifact(), received)
		return
	}
	if ctx.Err() != nil {
		status, reason := transferInterruption(ctx)
		for _, name := range prepared.GetTopLevel() {
			results.add(name, status, reason)
		}
		cleanupTransferArtifacts(ctx, events, s, src, dst, results, id, prepared.GetArtifact(), received)
		return
	}

	s.applyBundle(ctx, events, dst, in, prepared, received, id, results)
	cleanupTransferArtifacts(ctx, events, s, src, dst, results, id, prepared.GetArtifact(), received)
}
func checkBundleBounds(prepared *agentv1.PrepareBundleResponse, bounds TransferBounds, selected int) error {
	if bounds.MaxBundleBytes > 0 && prepared.GetCompressedBytes() > bounds.MaxBundleBytes {
		return fmt.Errorf("%w: the bundle is larger than the negotiated transport limit", ErrTooLarge)
	}
	if bounds.MaxExpandedBytes > 0 && prepared.GetExpandedBytes() > bounds.MaxExpandedBytes {
		return fmt.Errorf("%w: the bundle expands beyond the negotiated limit", ErrArchiveLimit)
	}
	if bounds.MaxEntries > 0 && prepared.GetEntryCount() > bounds.MaxEntries {
		return fmt.Errorf("%w: the bundle has more entries than the negotiated limit", ErrArchiveLimit)
	}
	if bounds.MaxSources > 0 && uint32(selected) > bounds.MaxSources {
		return fmt.Errorf("%w: the selection exceeds the negotiated source limit", ErrValidation)
	}
	return nil
}

func transferSourcePaths(in transferInput) []string {
	paths := make([]string, 0, len(in.sources))
	for _, name := range in.sources {
		paths = append(paths, in.sourcePath(name))
	}
	return paths
}

func (s *Service) relayBundle(ctx context.Context, events chan TransferEvent, src, dst FileClient, in transferInput, prepared *agentv1.PrepareBundleResponse, id string) (string, error) {
	srcCtx, srcCancel := context.WithCancel(ctx)
	defer srcCancel()
	dstCtx, dstCancel := context.WithCancel(ctx)
	defer dstCancel()

	read, err := src.ReadArtifact(srcCtx, &agentv1.ReadArtifactRequest{TransferId: id, Artifact: prepared.GetArtifact()})
	if err != nil {
		return "", mapTransferRPCError(err)
	}
	receive, err := dst.ReceiveArtifact(dstCtx)
	if err != nil {
		return "", mapTransferRPCError(err)
	}
	header := &agentv1.ReceiveArtifactRequest{Part: &agentv1.ReceiveArtifactRequest_Header{Header: &agentv1.ReceiveArtifactHeader{
		TransferId:    id,
		Root:          in.destRoot,
		Directory:     in.destDir,
		Length:        prepared.GetCompressedBytes(),
		Sha256:        prepared.GetSha256(),
		ExpandedBytes: prepared.GetExpandedBytes(),
		EntryCount:    prepared.GetEntryCount(),
		TopLevel:      prepared.GetTopLevel(),
	}}}
	if err := receive.Send(header); err != nil {
		return "", mapTransferRPCError(err)
	}

	chunks := make(chan []byte, transferChunkBuffer)
	readErr := make(chan error, 1)
	go func() {
		defer close(chunks)
		for {
			chunk, recvErr := read.Recv()
			if errors.Is(recvErr, io.EOF) {
				readErr <- nil
				return
			}
			if recvErr != nil {
				readErr <- mapTransferRPCError(recvErr)
				return
			}
			data := chunk.GetData()
			if len(data) == 0 {
				continue
			}
			select {
			case chunks <- data:
			case <-srcCtx.Done():
				readErr <- srcCtx.Err()
				return
			}
		}
	}()

	total := int64(prepared.GetCompressedBytes())
	var sent int64
	last := time.Now()
	for data := range chunks {
		sent += int64(len(data))
		if err := receive.Send(&agentv1.ReceiveArtifactRequest{Part: &agentv1.ReceiveArtifactRequest_Data{Data: data}}); err != nil {
			srcCancel()
			<-readErr
			return "", mapTransferRPCError(err)
		}
		if time.Since(last) >= transferProgress {
			last = time.Now()
			emitTransferPhase(ctx, events, TransferPhaseTransferring, sent, &total)
		}
	}
	if err := <-readErr; err != nil {
		dstCancel()
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("%w: %v", ErrTransferTimeout, err)
			}
			return "", fmt.Errorf("%w: %v", ErrTransferCancelled, err)
		}
		return "", err
	}
	emitTransferPhase(ctx, events, TransferPhaseTransferring, sent, &total)
	response, err := receive.CloseAndRecv()
	if err != nil {
		if status.Code(err) == codes.Aborted {
			// The destination refused the bundle itself (digest, length or a
			// truncated stream); nothing was published, so this is not a
			// destination conflict.
			return "", fmt.Errorf("%w: %v", ErrValidation, err)
		}
		return "", mapTransferRPCError(err)
	}
	if response.GetArtifact() == "" {
		return "", fmt.Errorf("%w: the destination did not stage the bundle", ErrAgentUnavailable)
	}
	return response.GetArtifact(), nil
}

func (s *Service) applyBundle(ctx context.Context, events chan TransferEvent, dst FileClient, in transferInput, prepared *agentv1.PrepareBundleResponse, artifact, id string, results *transferResults) {
	stream, err := dst.ApplyArtifact(ctx, &agentv1.ApplyArtifactRequest{
		TransferId: id,
		Artifact:   artifact,
		Root:       in.destRoot,
		Directory:  in.destDir,
		TopLevel:   prepared.GetTopLevel(),
	})
	if err != nil {
		err = mapTransferRPCError(err)
		for _, name := range prepared.GetTopLevel() {
			results.add(name, transferFailureStatus(err), transferFailureReason(err))
		}
		return
	}
	pending := make(map[string]struct{}, len(prepared.GetTopLevel()))
	for _, name := range prepared.GetTopLevel() {
		pending[name] = struct{}{}
	}
	inFlight := ""
	for {
		event, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			for _, name := range prepared.GetTopLevel() {
				if _, ok := pending[name]; !ok {
					continue
				}
				switch {
				case name == inFlight:
					results.add(name, TransferStatusUncertain, "publication acknowledgement was lost")
				case ctx.Err() != nil:
					status, reason := transferInterruption(ctx)
					results.add(name, status, reason)
				default:
					results.add(name, transferFailureStatus(mapTransferRPCError(recvErr)), transferFailureReason(recvErr))
				}
			}
			return
		}
		switch part := event.GetEvent().(type) {
		case *agentv1.ApplyArtifactEvent_Phase:
			phase := part.Phase.GetPhase()
			switch phase {
			case "extract":
				emitTransferPhase(ctx, events, TransferPhaseExtracting, part.Phase.GetBytesDone(), int64Pointer(int64(prepared.GetExpandedBytes())))
			case "publish":
				inFlight = part.Phase.GetPath()
				emitTransferPhase(ctx, events, TransferPhasePublishing, part.Phase.GetBytesDone(), int64Pointer(int64(prepared.GetExpandedBytes())))
			}
		case *agentv1.ApplyArtifactEvent_Outcome:
			name := part.Outcome.GetName()
			delete(pending, name)
			if name == inFlight {
				inFlight = ""
			}
			switch part.Outcome.GetStatus() {
			case agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_PUBLISHED:
				results.add(name, TransferStatusCopied, "")
			case agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_SKIPPED:
				results.add(name, TransferStatusConflict, part.Outcome.GetReason())
			default:
				results.add(name, TransferStatusFailed, part.Outcome.GetReason())
			}
		}
	}
	for _, name := range prepared.GetTopLevel() {
		if _, ok := pending[name]; !ok {
			continue
		}
		switch {
		case name == inFlight:
			results.add(name, TransferStatusUncertain, "publication acknowledgement was lost")
		case ctx.Err() != nil:
			status, reason := transferInterruption(ctx)
			results.add(name, status, reason)
		default:
			results.add(name, TransferStatusFailed, "the destination did not acknowledge the entry")
		}
	}
}

func transferInterruption(ctx context.Context) (string, string) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return TransferStatusFailed, "timeout"
	}
	return TransferStatusCancelled, "cancelled"
}

func cleanupTransferArtifacts(ctx context.Context, events chan TransferEvent, s *Service, src, dst FileClient, results *transferResults, id, sourceArtifact, destArtifact string) {
	emitTransferPhase(ctx, events, TransferPhaseCleaningUp, 0, nil)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transferCleanupTimeout)
	defer cancel()
	if destArtifact != "" {
		if err := discardTransferArtifact(cleanupCtx, dst, id, destArtifact); err != nil {
			results.cleanupFailures++
			s.logger.Warn("files: transfer cleanup failed", "transfer_id", id, "side", "destination", "error", err)
		}
	}
	if sourceArtifact != "" {
		if err := discardTransferArtifact(cleanupCtx, src, id, sourceArtifact); err != nil {
			results.cleanupFailures++
			s.logger.Warn("files: transfer cleanup failed", "transfer_id", id, "side", "source", "error", err)
		}
	}
}

func discardTransferArtifact(ctx context.Context, client FileClient, id, artifact string) error {
	if artifact == "" {
		return nil
	}
	_, err := client.DiscardArtifact(ctx, &agentv1.DiscardArtifactRequest{TransferId: id, Artifact: artifact})
	if err == nil || status.Code(err) == codes.NotFound {
		return nil
	}
	return mapTransferRPCError(err)
}

func transferFailureStatus(err error) string {
	switch {
	case errors.Is(err, ErrTransferCancelled):
		return TransferStatusCancelled
	case errors.Is(err, ErrConflict):
		return TransferStatusConflict
	default:
		return TransferStatusFailed
	}
}

// transferAcknowledgementStatus classifies a failure after the commit message
// was sent. The destination may already hold the entry, so a missing or
// transport-level acknowledgement is uncertain even when the request context
// is canceled or its deadline expired. A typed refusal from the destination is
// still a definite conflict or failure.
func transferAcknowledgementStatus(err error) string {
	if errors.Is(err, ErrConflict) {
		return TransferStatusConflict
	}
	if errors.Is(err, ErrAgentUnavailable) || errors.Is(err, ErrTransferTimeout) {
		return TransferStatusUncertain
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Unknown, codes.Internal, codes.Canceled:
		return TransferStatusUncertain
	}
	return TransferStatusFailed
}

func transferFailureReason(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrTransferTimeout):
		return "timeout"
	case errors.Is(err, ErrTransferCancelled):
		return "cancelled"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrInsufficientStorage):
		return "insufficient_storage"
	case errors.Is(err, ErrArchiveLimit):
		return "archive_limit"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrAgentOutdated):
		return "agent_outdated"
	case errors.Is(err, ErrDisabledOnNode):
		return "file_manager_disabled"
	case errors.Is(err, ErrAgentUnavailable):
		return "agent_unavailable"
	case errors.Is(err, ErrDisabled):
		return "disabled"
	}
	switch status.Code(err) {
	case codes.NotFound:
		return "not_found"
	case codes.PermissionDenied:
		return "forbidden"
	case codes.InvalidArgument:
		return "invalid"
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		return "conflict"
	case codes.ResourceExhausted:
		return "busy"
	case codes.Unimplemented:
		return "agent_outdated"
	case codes.Unavailable:
		return "agent_unavailable"
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.Canceled:
		return "cancelled"
	}
	return publicError(mapTransferRPCError(err))
}

func int64Pointer(value int64) *int64 { return &value }

func (s *Service) auditTransfer(in transferInput, summary TransferSummary) {
	s.logger.Info("files: audit",
		"op", "transfer",
		"result", summary.Status,
		"server_id", in.sourceID.String(),
		"root", in.sourceRoot,
		"relpath", in.sourceDir,
		"destination_server_id", in.destinationID.String(),
		"destination_root", in.destRoot,
		"destination_path", in.destDir,
		"transfer_id", summary.TransferID,
		"mode", summary.Mode,
		"copied", summary.Copied,
		"conflicts", summary.Conflicts,
		"skipped", summary.Skipped,
		"failed", summary.Failed,
		"cancelled", summary.Cancelled,
		"uncertain", summary.Uncertain,
		"cleanup_failures", summary.CleanupFailures,
	)
}
