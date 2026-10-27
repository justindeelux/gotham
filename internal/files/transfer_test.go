package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func transferCapabilitiesResponse() *agentv1.TransferCapabilitiesResponse {
	return &agentv1.TransferCapabilitiesResponse{
		Available:        true,
		MaxBundleBytes:   1 << 30,
		MaxExpandedBytes: 4 << 30,
		MaxEntries:       100_000,
		MaxSources:       1024,
		MaxConcurrency:   1,
	}
}

func transferRootsResponse() *agentv1.ListRootsResponse {
	return &agentv1.ListRootsResponse{Roots: []*agentv1.FileRoot{
		{Name: "volumes", Path: "/volumes", Writable: true, FreeBytes: 1 << 40},
	}}
}

func fileStat(etag string) *agentv1.StatResponse {
	return &agentv1.StatResponse{Entry: &agentv1.FileEntry{
		Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 5, Etag: etag,
		Mode: 0o644, ModifiedAt: timestamppb.New(time.Unix(1_700_000_000, 0)),
	}}
}

func dirStat(name string) *agentv1.StatResponse {
	return &agentv1.StatResponse{Entry: &agentv1.FileEntry{
		Name: name, Kind: agentv1.FileKind_FILE_KIND_DIRECTORY, Mode: 0o755,
		ModifiedAt: timestamppb.New(time.Unix(1_700_000_000, 0)),
	}}
}

func newTransferTestService(t *testing.T, src, dst *fakeFileClient) (*Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	t.Setenv(FeatureEnv, "true")
	sourceID, destinationID := uuid.New(), uuid.New()
	registry := newFakeRegistry()
	registry.items[sourceID] = &servers.Server{ID: sourceID, Name: "source", Status: servers.StatusReady}
	registry.items[destinationID] = &servers.Server{ID: destinationID, Name: "destination", Status: servers.StatusReady}
	svc := NewService(Config{
		Registry:       registry,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxUploadBytes: DefaultMaxUploadBytes,
		Dial: func(_ context.Context, server *servers.Server) (FileClient, error) {
			if server.ID == sourceID {
				return src, nil
			}
			return dst, nil
		},
	})
	return svc, sourceID, destinationID
}

func transferRequest(destinationID uuid.UUID, sources ...string) TransferRequest {
	return TransferRequest{
		SourceRoot:      "volumes",
		SourceDirectory: "data",
		Sources:         sources,
		Destination:     TransferDestination{ServerID: destinationID, Root: "volumes", Path: "incoming"},
	}
}

func collectTransferEvents(t *testing.T, run *TransferRun) []TransferEvent {
	t.Helper()
	var events []TransferEvent
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-run.Events:
			if !open {
				return events
			}
			events = append(events, event)
		case <-deadline:
			t.Fatal("transfer events did not close")
		}
	}
}

func summaryOf(t *testing.T, events []TransferEvent) TransferSummary {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no transfer events")
	}
	last := events[len(events)-1]
	if last.Type != TransferEventSummary || last.Summary == nil {
		t.Fatalf("last event = %+v, want the terminal summary", last)
	}
	return *last.Summary
}

func phasesOf(events []TransferEvent) []string {
	var phases []string
	for _, event := range events {
		if event.Type == TransferEventPhase {
			phases = append(phases, event.Phase)
		}
	}
	return phases
}

func directFakes(t *testing.T) (*fakeFileClient, *fakeFileClient) {
	t.Helper()
	src := &fakeFileClient{
		capResp:   transferCapabilitiesResponse(),
		rootsResp: transferRootsResponse(),
		readBody:  []byte("hello"),
		statHook: func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
			if in.GetPath() == "data" {
				return dirStat("data"), nil
			}
			return fileStat(`"etag-1"`), nil
		},
	}
	dst := &fakeFileClient{
		capResp:     transferCapabilitiesResponse(),
		rootsResp:   transferRootsResponse(),
		writeResp:   &agentv1.WriteResponse{Entry: fileStat(`"etag-1"`).GetEntry(), Created: true},
		applyEvents: []*agentv1.ApplyArtifactEvent{},
		statHook: func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
			if in.GetPath() == "incoming" {
				return dirStat("incoming"), nil
			}
			return nil, status.Error(codes.NotFound, "missing")
		},
	}
	return src, dst
}

func TestPlanTransferIsReadOnly(t *testing.T) {
	src, dst := directFakes(t)
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	plan, err := svc.PlanTransfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Mode != TransferModeDirect || len(plan.Sources) != 1 || plan.Sources[0].Name != "a.txt" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.AvailableBytes == nil || *plan.AvailableBytes == 0 {
		t.Fatalf("plan available bytes = %v", plan.AvailableBytes)
	}
	if src.seenPrepareRequest() != nil {
		t.Fatal("plan prepared a bundle")
	}
	if dst.lastWriteStream() != nil || dst.lastReceiveStream() != nil || len(dst.discardedArtifacts()) != 0 {
		t.Fatal("plan touched destination staging")
	}
}

func TestPlanTransferReportsConflictsAndUnsupported(t *testing.T) {
	src, dst := directFakes(t)
	src.statHook = func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
		switch {
		case in.GetPath() == "data":
			return dirStat("data"), nil
		case strings.HasSuffix(in.GetPath(), "link"):
			return &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "link", Kind: agentv1.FileKind_FILE_KIND_SYMLINK}}, nil
		case strings.HasSuffix(in.GetPath(), "a.txt"):
			return fileStat(`"e"`), nil
		default:
			return dirStat(in.GetPath()), nil
		}
	}
	dst.statHook = func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
		if in.GetPath() == "incoming" {
			return dirStat("incoming"), nil
		}
		if strings.HasSuffix(in.GetPath(), "a.txt") {
			return fileStat(`"other"`), nil
		}
		return nil, status.Error(codes.NotFound, "missing")
	}
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	plan, err := svc.PlanTransfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0] != "a.txt" {
		t.Fatalf("conflicts = %v", plan.Conflicts)
	}

	src.statHook = func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
		if in.GetPath() == "data" {
			return dirStat("data"), nil
		}
		return &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "link", Kind: agentv1.FileKind_FILE_KIND_SYMLINK}}, nil
	}
	dst.statHook = func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
		if in.GetPath() == "incoming" {
			return dirStat("incoming"), nil
		}
		return nil, status.Error(codes.NotFound, "missing")
	}
	plan, err = svc.PlanTransfer(context.Background(), sourceID, transferRequest(destinationID, "link"))
	if err != nil {
		t.Fatalf("plan unsupported: %v", err)
	}
	if plan.Mode != TransferModeBundle || len(plan.Unsupported) != 1 || plan.Unsupported[0].Name != "link" {
		t.Fatalf("plan = %+v, want the symlink reported unsupported", plan)
	}
}

func TestDirectTransferCopiesWithExclusiveCreate(t *testing.T) {
	src, dst := directFakes(t)
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Status != TransferStatusCopied || summary.Copied != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	stream := dst.lastWriteStream()
	if stream == nil {
		t.Fatal("destination received no write")
	}
	header, body, commit, closes := stream.snapshot()
	if header.GetRoot() != "volumes" || header.GetPath() != "incoming/a.txt" || header.GetOverwrite() {
		t.Fatalf("write header = %+v", header)
	}
	if header.GetSize() != 5 || string(body) != "hello" {
		t.Fatalf("wrote %q with declared size %d", body, header.GetSize())
	}
	sum := sha256.Sum256([]byte("hello"))
	if commit.GetSha256() != hex.EncodeToString(sum[:]) {
		t.Fatalf("commit sha = %q", commit.GetSha256())
	}
	if closes != 1 {
		t.Fatalf("close calls = %d, want the commit acknowledgement", closes)
	}
	if read := src.seenReadRequest(); read == nil || read.GetPath() != "data/a.txt" {
		t.Fatalf("source read = %+v", read)
	}
}

func TestDirectTransferNeverOverwritesAConflict(t *testing.T) {
	src, dst := directFakes(t)
	dst.statHook = func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
		if in.GetPath() == "incoming" {
			return dirStat("incoming"), nil
		}
		return fileStat(`"existing"`), nil
	}
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Conflicts != 1 || summary.Status == TransferStatusCopied {
		t.Fatalf("summary = %+v", summary)
	}
	if dst.lastWriteStream() != nil {
		t.Fatal("a conflicting destination was written")
	}
}

func TestDirectTransferStopsWhenTheSourceChanges(t *testing.T) {
	src, dst := directFakes(t)
	var calls int32
	src.statHook = func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
		if in.GetPath() == "data" {
			return dirStat("data"), nil
		}
		if atomic.AddInt32(&calls, 1) == 1 {
			return fileStat(`"etag-1"`), nil
		}
		return fileStat(`"etag-2"`), nil
	}
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Failed != 1 || summary.Copied != 0 {
		t.Fatalf("summary = %+v, want a failed entry", summary)
	}
	stream := dst.lastWriteStream()
	if stream == nil {
		t.Fatal("destination stream missing")
	}
	if _, _, commit, closes := stream.snapshot(); commit != nil || closes != 0 {
		t.Fatal("a changed source was committed")
	}
}

func TestDirectTransferTruncatedSourceFails(t *testing.T) {
	src, dst := directFakes(t)
	src.readBody = []byte("hel")
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Failed != 1 || summary.Copied != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestDirectTransferLostAcknowledgementIsUncertain(t *testing.T) {
	src, dst := directFakes(t)
	dst.writeCloseErr = status.Error(codes.Unavailable, "connection lost")
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "a.txt"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Uncertain != 1 || summary.Status != TransferStatusUncertain {
		t.Fatalf("summary = %+v, want an uncertain entry", summary)
	}
}

func bundleFakes(t *testing.T) (*fakeFileClient, *fakeFileClient) {
	t.Helper()
	payload := []byte("0123456789")
	src := &fakeFileClient{
		capResp:   transferCapabilitiesResponse(),
		rootsResp: transferRootsResponse(),
		statResp:  dirStat("dir"),
		prepareResp: &agentv1.PrepareBundleResponse{
			TransferId:      "transfer-1",
			Artifact:        "source-artifact",
			CompressedBytes: uint64(len(payload)),
			Sha256:          strings.Repeat("a", 64),
			ExpandedBytes:   100,
			EntryCount:      2,
			TopLevel:        []string{"dir"},
			SkippedCount:    1,
			Skipped:         []*agentv1.SkippedEntry{{Path: "dir/link", Reason: "symlink"}},
		},
		artifactChunks: [][]byte{payload},
	}
	dst := &fakeFileClient{
		capResp:   transferCapabilitiesResponse(),
		rootsResp: transferRootsResponse(),
		statHook: func(in *agentv1.StatRequest) (*agentv1.StatResponse, error) {
			if in.GetPath() == "incoming" {
				return dirStat("incoming"), nil
			}
			return nil, status.Error(codes.NotFound, "missing")
		},
		receiveResp: &agentv1.ReceiveArtifactResponse{Artifact: "destination-artifact"},
		applyEvents: []*agentv1.ApplyArtifactEvent{
			{Event: &agentv1.ApplyArtifactEvent_Phase{Phase: &agentv1.TransferPhase{Phase: "extract"}}},
			{Event: &agentv1.ApplyArtifactEvent_Phase{Phase: &agentv1.TransferPhase{Phase: "publish", Path: "dir"}}},
			{Event: &agentv1.ApplyArtifactEvent_Outcome{Outcome: &agentv1.TransferOutcome{
				Name: "dir", Status: agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_PUBLISHED,
			}}},
			{Event: &agentv1.ApplyArtifactEvent_Phase{Phase: &agentv1.TransferPhase{Phase: "done", Path: "."}}},
		},
	}
	return src, dst
}

func TestBundleTransferRelaysAppliesAndDiscards(t *testing.T) {
	src, dst := bundleFakes(t)
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "dir"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	events := collectTransferEvents(t, run)
	summary := summaryOf(t, events)
	if summary.Status != TransferStatusCopied || summary.Copied != 1 || summary.SkippedCount != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	phases := strings.Join(phasesOf(events), ",")
	for _, want := range []string{TransferPhasePreparing, TransferPhaseTransferring, TransferPhaseExtracting, TransferPhasePublishing, TransferPhaseCleaningUp} {
		if !strings.Contains(phases, want) {
			t.Fatalf("phases = %q, missing %q", phases, want)
		}
	}
	receive := dst.lastReceiveStream()
	if receive == nil {
		t.Fatal("destination receive stream missing")
	}
	header, body := receive.snapshot()
	if header.GetLength() != 10 || header.GetDirectory() != "incoming" || string(body) != "0123456789" {
		t.Fatalf("receive header=%+v body=%q", header, body)
	}
	if got := strings.Join(src.discardedArtifacts(), ","); got != "source-artifact" {
		t.Fatalf("source discarded = %q", got)
	}
	if got := strings.Join(dst.discardedArtifacts(), ","); got != "destination-artifact" {
		t.Fatalf("destination discarded = %q", got)
	}
}

func TestBundleTransferRejectsOverLimitBundleBeforeSending(t *testing.T) {
	src, dst := bundleFakes(t)
	dst.capResp = transferCapabilitiesResponse()
	dst.capResp.MaxExpandedBytes = 50
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "dir"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Failed != 1 || summary.Copied != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if dst.lastReceiveStream() != nil {
		t.Fatal("an over-limit bundle was sent")
	}
	if got := strings.Join(src.discardedArtifacts(), ","); got != "source-artifact" {
		t.Fatalf("source staging was not discarded: %q", got)
	}
}

func TestTransferCancellationCleansBothSides(t *testing.T) {
	for _, phase := range []string{"transferring", "extracting", "publishing"} {
		t.Run(phase, func(t *testing.T) {
			src, dst := bundleFakes(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch phase {
			case "transferring":
				src.onArtifactRecv = func() { cancel() }
			case "extracting":
				var once sync.Once
				dst.applyHook = func(index int) {
					if index == 0 {
						once.Do(cancel)
					}
				}
			case "publishing":
				var once sync.Once
				dst.applyHook = func(index int) {
					if index == 1 {
						once.Do(cancel)
					}
				}
			}
			svc, sourceID, destinationID := newTransferTestService(t, src, dst)

			run, err := svc.Transfer(ctx, sourceID, transferRequest(destinationID, "dir"))
			if err != nil {
				t.Fatalf("transfer: %v", err)
			}
			collectTransferEvents(t, run)
			deadline := time.After(5 * time.Second)
			for len(src.discardedArtifacts()) == 0 || (phase == "publishing" && len(dst.discardedArtifacts()) == 0) {
				select {
				case <-deadline:
					t.Fatalf("cleanup did not run (source %v dest %v)", src.discardedArtifacts(), dst.discardedArtifacts())
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}

func TestBundleRelayKeepsMemoryBounded(t *testing.T) {
	const chunkSize = 64 << 10
	const chunkCount = 16
	chunks := make([][]byte, 0, chunkCount)
	var payloadSize int
	for i := 0; i < chunkCount; i++ {
		chunk := make([]byte, chunkSize)
		for j := range chunk {
			chunk[j] = byte(i)
		}
		chunks = append(chunks, chunk)
		payloadSize += len(chunk)
	}
	src, dst := bundleFakes(t)
	src.artifactChunks = chunks
	src.prepareResp.CompressedBytes = uint64(payloadSize)
	var produced atomic.Int64
	src.onArtifactRecv = func() { produced.Add(1) }

	gate := make(chan struct{})
	var blocked atomic.Bool
	var producedWhileBlocked atomic.Int64
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)
	// Block the relay's first body send so the source tries to run ahead.
	dst.receiveHook = func(msg *agentv1.ReceiveArtifactRequest) error {
		if msg.GetData() == nil || !blocked.CompareAndSwap(false, true) {
			return nil
		}
		producedWhileBlocked.Store(produced.Load())
		<-gate
		return nil
	}
	type transferResult struct {
		run *TransferRun
		err error
	}
	results := make(chan transferResult, 1)
	go func() {
		run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "dir"))
		results <- transferResult{run: run, err: err}
	}()
	var result transferResult
	select {
	case result = <-results:
	case <-time.After(10 * time.Second):
		t.Fatal("transfer did not start")
	}
	if result.err != nil {
		t.Fatalf("transfer: %v", result.err)
	}
	deadline := time.After(5 * time.Second)
	for !blocked.Load() {
		select {
		case <-deadline:
			close(gate)
			t.Fatal("destination send was never blocked")
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(gate)
	summary := summaryOf(t, collectTransferEvents(t, result.run))
	if summary.Copied != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := producedWhileBlocked.Load(); got > int64(transferChunkBuffer)+2 {
		t.Fatalf("source produced %d chunks while the destination was blocked, want at most %d", got, transferChunkBuffer+2)
	} else if got < 2 {
		t.Fatalf("source did not buffer a chunk (produced %d)", got)
	}
}

func TestTransferErrorMappingIsDistinct(t *testing.T) {
	tests := map[string]struct {
		err  error
		want error
	}{
		"outdated":           {status.Error(codes.Unimplemented, "unsupported"), ErrAgentOutdated},
		"disabled node":      {status.Error(codes.FailedPrecondition, DisabledReason), ErrDisabledOnNode},
		"busy":               {status.Error(codes.ResourceExhausted, "too many transfers"), ErrBusy},
		"insufficient space": {status.Error(codes.ResourceExhausted, "low on free space"), ErrInsufficientStorage},
		"timeout":            {status.Error(codes.DeadlineExceeded, "deadline"), ErrTransferTimeout},
		"unavailable":        {status.Error(codes.Unavailable, "down"), ErrAgentUnavailable},
		"conflict":           {status.Error(codes.AlreadyExists, "exists"), ErrConflict},
	}
	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			if err := mapTransferRPCError(tc.err); !errors.Is(err, tc.want) {
				t.Fatalf("mapped %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPlanTransferRequiresBundleCapabilities(t *testing.T) {
	src, dst := bundleFakes(t)
	src.capErr = status.Error(codes.Unimplemented, "unsupported")
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	if _, err := svc.PlanTransfer(context.Background(), sourceID, transferRequest(destinationID, "dir")); !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("plan on an outdated source = %v, want ErrAgentOutdated", err)
	}
}

func TestBundleTransferReportsLostPublicationAcknowledgement(t *testing.T) {
	src, dst := bundleFakes(t)
	dst.applyEvents = []*agentv1.ApplyArtifactEvent{
		{Event: &agentv1.ApplyArtifactEvent_Phase{Phase: &agentv1.TransferPhase{Phase: "publish", Path: "dir"}}},
	}
	dst.applyStreamErr = status.Error(codes.Unavailable, "connection lost")
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "dir"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Uncertain != 1 || summary.Status != TransferStatusUncertain {
		t.Fatalf("summary = %+v, want the in-flight publication marked uncertain", summary)
	}
}

func TestBundleTransferDestinationRejectsTheStream(t *testing.T) {
	src, dst := bundleFakes(t)
	dst.receiveCloseErr = status.Error(codes.Aborted, "the bundle digest does not match")
	svc, sourceID, destinationID := newTransferTestService(t, src, dst)

	run, err := svc.Transfer(context.Background(), sourceID, transferRequest(destinationID, "dir"))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	summary := summaryOf(t, collectTransferEvents(t, run))
	if summary.Failed != 1 || summary.Copied != 0 {
		t.Fatalf("summary = %+v, want a failed entry", summary)
	}
	if got := strings.Join(src.discardedArtifacts(), ","); got != "source-artifact" {
		t.Fatalf("source staging was not discarded after a rejected bundle: %q", got)
	}
}
