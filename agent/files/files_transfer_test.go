package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"archive/tar"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func newTransferServer(t *testing.T, rootDir string) *FileServer {
	t.Helper()
	server, err := NewFileServer(Config{
		ManagedVolumeRoot: rootDir,
		ComposeRoot:       t.TempDir(),
		CertDir:           filepath.Join(t.TempDir(), "certs"),
		AgentStateRoot:    t.TempDir(),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func prepareBundle(t *testing.T, server *FileServer, transferID string, sources ...string) *agentv1.PrepareBundleResponse {
	t.Helper()
	resp, err := server.PrepareBundle(context.Background(), &agentv1.PrepareBundleRequest{
		TransferId: transferID, Root: RootVolumes, Sources: sources,
	})
	if err != nil {
		t.Fatalf("PrepareBundle: %v", err)
	}
	return resp
}

type collectArtifactStream struct {
	grpc.ServerStream
	buf bytes.Buffer
	ctx context.Context
}

func (s *collectArtifactStream) Send(chunk *agentv1.ArtifactChunk) error {
	_, err := s.buf.Write(chunk.GetData())
	return err
}

func (s *collectArtifactStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func readArtifact(t *testing.T, server *FileServer, transferID, handle string) []byte {
	t.Helper()
	stream := &collectArtifactStream{}
	if err := server.ReadArtifact(&agentv1.ReadArtifactRequest{TransferId: transferID, Artifact: handle}, stream); err != nil {
		t.Fatalf("ReadArtifact: %v", err)
	}
	return stream.buf.Bytes()
}

type scriptedReceiveStream struct {
	grpc.ServerStream
	msgs []*agentv1.ReceiveArtifactRequest
	next int
	resp *agentv1.ReceiveArtifactResponse
	ctx  context.Context
}

func (s *scriptedReceiveStream) Recv() (*agentv1.ReceiveArtifactRequest, error) {
	if s.next >= len(s.msgs) {
		return nil, io.EOF
	}
	msg := s.msgs[s.next]
	s.next++
	return msg, nil
}

func (s *scriptedReceiveStream) SendAndClose(resp *agentv1.ReceiveArtifactResponse) error {
	s.resp = resp
	return nil
}

func (s *scriptedReceiveStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func receiveMessages(header *agentv1.ReceiveArtifactHeader, body []byte) []*agentv1.ReceiveArtifactRequest {
	msgs := []*agentv1.ReceiveArtifactRequest{{Part: &agentv1.ReceiveArtifactRequest_Header{Header: header}}}
	const chunk = 4096
	for off := 0; off < len(body); off += chunk {
		end := min(off+chunk, len(body))
		msgs = append(msgs, &agentv1.ReceiveArtifactRequest{Part: &agentv1.ReceiveArtifactRequest_Data{Data: body[off:end]}})
	}
	return msgs
}

func receiveHeader(transferID string, prep *agentv1.PrepareBundleResponse, body []byte) *agentv1.ReceiveArtifactHeader {
	sum := sha256.Sum256(body)
	return &agentv1.ReceiveArtifactHeader{
		TransferId:    transferID,
		Root:          RootVolumes,
		Directory:     ".",
		Length:        uint64(len(body)),
		Sha256:        hex.EncodeToString(sum[:]),
		ExpandedBytes: prep.GetExpandedBytes(),
		EntryCount:    prep.GetEntryCount(),
		TopLevel:      prep.GetTopLevel(),
	}
}

func receiveArtifact(t *testing.T, server *FileServer, transferID string, prep *agentv1.PrepareBundleResponse, body []byte) *agentv1.ReceiveArtifactResponse {
	t.Helper()
	stream := &scriptedReceiveStream{msgs: receiveMessages(receiveHeader(transferID, prep, body), body)}
	if err := server.ReceiveArtifact(stream); err != nil {
		t.Fatalf("ReceiveArtifact: %v", err)
	}
	if stream.resp == nil {
		t.Fatal("ReceiveArtifact returned no response")
	}
	return stream.resp
}

type collectApplyStream struct {
	grpc.ServerStream
	events []*agentv1.ApplyArtifactEvent
	ctx    context.Context
}

func (s *collectApplyStream) Send(event *agentv1.ApplyArtifactEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *collectApplyStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *collectApplyStream) outcomes() map[string]agentv1.TransferOutcomeStatus {
	out := make(map[string]agentv1.TransferOutcomeStatus)
	for _, event := range s.events {
		if outcome := event.GetOutcome(); outcome != nil {
			out[outcome.GetName()] = outcome.GetStatus()
		}
	}
	return out
}

func applyArtifact(server *FileServer, transferID string, received *agentv1.ReceiveArtifactResponse, dir string, topLevel []string, ctx context.Context) (*collectApplyStream, error) {
	stream := &collectApplyStream{ctx: ctx}
	err := server.ApplyArtifact(&agentv1.ApplyArtifactRequest{
		TransferId: transferID,
		Artifact:   received.GetArtifact(),
		Root:       RootVolumes,
		Directory:  dir,
		TopLevel:   topLevel,
	}, stream)
	return stream, err
}

func writeTreeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func assertNoTransferStaging(t *testing.T, rootDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(rootDir, fileTransferDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("transfer staging leaked: %v", entries)
	}
}

func TestTransferCapabilities(t *testing.T) {
	t.Parallel()
	server := newTransferServer(t, t.TempDir())
	resp, err := server.TransferCapabilities(context.Background(), &agentv1.TransferCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("TransferCapabilities: %v", err)
	}
	if !resp.GetAvailable() || resp.GetMaxBundleBytes() == 0 || resp.GetMaxExpandedBytes() == 0 ||
		resp.GetMaxEntries() == 0 || resp.GetMaxSources() != fileTransferMaxSources || resp.GetMaxConcurrency() == 0 {
		t.Fatalf("capabilities = %+v", resp)
	}
}

func TestTransferReadOnlySourceRoundTrip(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "alpha.txt", "hello")
	writeTreeFile(t, srcDir, "docs/readme.md", "read me")
	writeTreeFile(t, srcDir, "unicode-π.txt", "pi")
	if err := os.MkdirAll(filepath.Join(srcDir, "docs", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(srcDir, "alpha.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alpha.txt", filepath.Join(srcDir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(srcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(srcDir, 0o755) })

	source := newTransferServer(t, srcDir)
	destination := newTransferServer(t, t.TempDir())

	prep := prepareBundle(t, source, "transfer-readonly", "alpha.txt", "docs", "unicode-π.txt", "link")
	if got, want := strings.Join(prep.GetTopLevel(), ","), "alpha.txt,docs,unicode-π.txt"; got != want {
		t.Fatalf("top_level = %q, want %q", got, want)
	}
	if prep.GetSkippedCount() != 1 || len(prep.GetSkipped()) != 1 ||
		prep.GetSkipped()[0].GetPath() != "link" || prep.GetSkipped()[0].GetReason() != "symlink" {
		t.Fatalf("skipped = %+v count %d", prep.GetSkipped(), prep.GetSkippedCount())
	}
	body := readArtifact(t, source, "transfer-readonly", prep.GetArtifact())
	if int64(len(body)) != int64(prep.GetCompressedBytes()) {
		t.Fatalf("bundle bytes = %d, want %d", len(body), prep.GetCompressedBytes())
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != prep.GetSha256() {
		t.Fatal("bundle digest does not match the response")
	}
	if entries, err := os.ReadDir(srcDir); err != nil || len(entries) != 4 {
		t.Fatalf("source root changed: entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, fileTransferDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepare wrote staging inside the source root: %v", err)
	}

	received := receiveArtifact(t, destination, "transfer-readonly", prep, body)
	stream, err := applyArtifact(destination, "transfer-readonly", received, ".", prep.GetTopLevel(), context.Background())
	if err != nil {
		t.Fatalf("ApplyArtifact: %v", err)
	}
	outcomes := stream.outcomes()
	for _, name := range prep.GetTopLevel() {
		if outcomes[name] != agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_PUBLISHED {
			t.Fatalf("outcome[%q] = %v", name, outcomes[name])
		}
	}
	destDir := destination.roots[RootVolumes].Path()
	if got, err := os.ReadFile(filepath.Join(destDir, "alpha.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("alpha.txt = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(destDir, "alpha.txt")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("alpha.txt mode = %v, %v", info, err)
	}
	if got, err := os.ReadFile(filepath.Join(destDir, "docs", "readme.md")); err != nil || string(got) != "read me" {
		t.Fatalf("docs/readme.md = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(destDir, "docs", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory missing: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(destDir, "unicode-π.txt")); err != nil || string(got) != "pi" {
		t.Fatalf("unicode file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "link")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink was copied: %v", err)
	}
	assertNoTransferStaging(t, destDir)

	if _, err := source.DiscardArtifact(context.Background(), &agentv1.DiscardArtifactRequest{
		TransferId: "transfer-readonly", Artifact: prep.GetArtifact(),
	}); err != nil {
		t.Fatalf("DiscardArtifact: %v", err)
	}
	if entries, err := os.ReadDir(source.transferRoot); err != nil || len(entries) != 0 {
		t.Fatalf("source staging not discarded: entries=%v err=%v", entries, err)
	}
}

func TestPrepareBundleRejectsInvalidSelections(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTreeFile(t, dir, "one/a.txt", "a")
	writeTreeFile(t, dir, "two/a.txt", "b")
	writeTreeFile(t, dir, "dir/child.txt", "c")
	server := newTransferServer(t, dir)

	cases := [][]string{
		nil,
		{"one/a.txt", "two/a.txt"},
		{"one", "one/a.txt"},
		{"dir", "dir/child.txt"},
		{"../escape"},
		{fileTransferDir + "/x"},
	}
	for _, sources := range cases {
		if _, err := server.PrepareBundle(context.Background(), &agentv1.PrepareBundleRequest{
			TransferId: "invalid", Root: RootVolumes, Sources: sources,
		}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("sources %v: code = %v, want InvalidArgument (%v)", sources, status.Code(err), err)
		}
	}

	many := make([]string, fileTransferMaxSources+1)
	for i := range many {
		many[i] = "f" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := server.PrepareBundle(context.Background(), &agentv1.PrepareBundleRequest{
		TransferId: "invalid", Root: RootVolumes, Sources: many,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("too many sources: code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestPrepareBundleAllSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := newTransferServer(t, dir)
	if _, err := server.PrepareBundle(context.Background(), &agentv1.PrepareBundleRequest{
		TransferId: "skipped", Root: RootVolumes, Sources: []string{"link"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("all-skipped selection: code = %v, want InvalidArgument (%v)", status.Code(err), err)
	}
	if entries, err := os.ReadDir(server.transferRoot); err == nil && len(entries) != 0 {
		t.Fatalf("failed prepare leaked staging: %v", entries)
	}
}

func TestPrepareBundleQuota(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTreeFile(t, dir, "big.txt", strings.Repeat("x", 4096))
	server, err := NewFileServer(Config{
		ManagedVolumeRoot:     dir,
		ComposeRoot:           t.TempDir(),
		CertDir:               filepath.Join(t.TempDir(), "certs"),
		AgentStateRoot:        t.TempDir(),
		FileArchiveMaxBytes:   64,
		FileArchiveMaxEntries: 8,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if _, err := server.PrepareBundle(context.Background(), &agentv1.PrepareBundleRequest{
		TransferId: "quota", Root: RootVolumes, Sources: []string{"big.txt"},
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("quota code = %v, want ResourceExhausted (%v)", status.Code(err), err)
	}
}

func TestTransferArtifactIsScopedToItsTransfer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTreeFile(t, dir, "a.txt", "a")
	server := newTransferServer(t, dir)
	prep := prepareBundle(t, server, "transfer-a", "a.txt")

	for _, tc := range []struct{ transferID, handle string }{
		{"transfer-b", prep.GetArtifact()},
		{"transfer-a", "000000000000000000000000000000000000000000000000"},
		{"transfer-a", ""},
	} {
		if err := server.ReadArtifact(&agentv1.ReadArtifactRequest{TransferId: tc.transferID, Artifact: tc.handle}, &collectArtifactStream{}); status.Code(err) != codes.NotFound && status.Code(err) != codes.InvalidArgument {
			t.Errorf("read (%q,%q): code = %v", tc.transferID, tc.handle, status.Code(err))
		}
	}
	if _, err := server.DiscardArtifact(context.Background(), &agentv1.DiscardArtifactRequest{TransferId: "transfer-b", Artifact: prep.GetArtifact()}); status.Code(err) != codes.NotFound {
		t.Errorf("discard from another transfer: code = %v, want NotFound", status.Code(err))
	}
	if body := readArtifact(t, server, "transfer-a", prep.GetArtifact()); len(body) == 0 {
		t.Fatal("the artifact was unreadable from its own transfer")
	}
}

func TestReceiveArtifactRejectsCorruptBundles(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", "hello")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "corrupt", "a.txt")
	body := readArtifact(t, source, "corrupt", prep.GetArtifact())

	destDir := t.TempDir()
	destination := newTransferServer(t, destDir)

	truncated := &scriptedReceiveStream{msgs: receiveMessages(receiveHeader("corrupt", prep, body), body[:len(body)-1])}
	if err := destination.ReceiveArtifact(truncated); status.Code(err) != codes.Aborted {
		t.Fatalf("truncated: code = %v, want Aborted (%v)", status.Code(err), err)
	}
	assertNoTransferStaging(t, destDir)

	badDigest := receiveHeader("corrupt", prep, body)
	badDigest.Sha256 = strings.Repeat("0", 64)
	if err := destination.ReceiveArtifact(&scriptedReceiveStream{msgs: receiveMessages(badDigest, body)}); status.Code(err) != codes.Aborted {
		t.Fatalf("bad digest: code = %v, want Aborted (%v)", status.Code(err), err)
	}
	assertNoTransferStaging(t, destDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := &scriptedReceiveStream{ctx: ctx, msgs: receiveMessages(receiveHeader("corrupt", prep, body), body)}
	if err := destination.ReceiveArtifact(cancelled); status.Code(err) != codes.Canceled {
		t.Fatalf("cancelled: code = %v, want Canceled (%v)", status.Code(err), err)
	}
	assertNoTransferStaging(t, destDir)
}

func TestReceiveArtifactRejectsOversizedBundle(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", strings.Repeat("x", 2048))
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "oversize", "a.txt")
	body := readArtifact(t, source, "oversize", prep.GetArtifact())

	destination, err := NewFileServer(Config{
		ManagedVolumeRoot:        t.TempDir(),
		ComposeRoot:              t.TempDir(),
		CertDir:                  filepath.Join(t.TempDir(), "certs"),
		AgentStateRoot:           t.TempDir(),
		FileArchiveMaxInputBytes: 32,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = destination.Close() })
	header := receiveHeader("oversize", prep, body)
	if err := destination.ReceiveArtifact(&scriptedReceiveStream{msgs: receiveMessages(header, body)}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized: code = %v, want ResourceExhausted (%v)", status.Code(err), err)
	}
}

func TestApplyArtifactSkipsConflictsAndPublishesTheRest(t *testing.T) {
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "keep.txt", "new keep")
	writeTreeFile(t, srcDir, "other.txt", "other")
	writeTreeFile(t, srcDir, "raced.txt", "raced")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "conflict", "keep.txt", "other.txt", "raced.txt")
	body := readArtifact(t, source, "conflict", prep.GetArtifact())

	destDir := t.TempDir()
	writeTreeFile(t, destDir, "keep.txt", "existing keep")
	destination := newTransferServer(t, destDir)
	received := receiveArtifact(t, destination, "conflict", prep, body)

	fileTransferPublishHook = func(name string) {
		if name == "other.txt" {
			writeTreeFile(t, destDir, "raced.txt", "created after preflight")
		}
	}
	t.Cleanup(func() { fileTransferPublishHook = nil })

	stream, err := applyArtifact(destination, "conflict", received, ".", prep.GetTopLevel(), context.Background())
	if err != nil {
		t.Fatalf("ApplyArtifact: %v", err)
	}
	outcomes := stream.outcomes()
	if outcomes["keep.txt"] != agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_SKIPPED {
		t.Fatalf("keep.txt outcome = %v, want SKIPPED", outcomes["keep.txt"])
	}
	if outcomes["other.txt"] != agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_PUBLISHED {
		t.Fatalf("other.txt outcome = %v, want PUBLISHED", outcomes["other.txt"])
	}
	if outcomes["raced.txt"] != agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_SKIPPED {
		t.Fatalf("raced.txt outcome = %v, want SKIPPED", outcomes["raced.txt"])
	}
	if got, _ := os.ReadFile(filepath.Join(destDir, "raced.txt")); string(got) != "created after preflight" {
		t.Fatalf("raced.txt was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(destDir, "keep.txt")); string(got) != "existing keep" {
		t.Fatalf("keep.txt was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(destDir, "other.txt")); string(got) != "other" {
		t.Fatalf("other.txt = %q", got)
	}
	assertNoTransferStaging(t, destDir)
}

func TestApplyArtifactRejectsMaliciousArchive(t *testing.T) {
	t.Parallel()
	body := buildTarGz(t, []tarEntry{
		{name: "escape/../../evil.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("pwn")},
	})
	sum := sha256.Sum256(body)
	header := &agentv1.ReceiveArtifactHeader{
		TransferId: "malicious",
		Root:       RootVolumes,
		Directory:  ".",
		Length:     uint64(len(body)),
		Sha256:     hex.EncodeToString(sum[:]),
		EntryCount: 1,
		TopLevel:   []string{"escape"},
	}
	destDir := t.TempDir()
	destination := newTransferServer(t, destDir)
	stream := &scriptedReceiveStream{msgs: receiveMessages(header, body)}
	if err := destination.ReceiveArtifact(stream); err != nil {
		t.Fatalf("ReceiveArtifact: %v", err)
	}
	if _, err := applyArtifact(destination, "malicious", stream.resp, ".", header.TopLevel, context.Background()); err == nil {
		t.Fatal("ApplyArtifact accepted an escaping archive")
	}
	if _, err := os.Stat(filepath.Join(destDir, "evil.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escaping entry was published: %v", err)
	}
	for _, entry := range mustReadDir(t, destDir) {
		if entry.Name() != fileTransferDir {
			t.Fatalf("destination changed: %q", entry.Name())
		}
	}
	assertNoTransferStaging(t, destDir)

	mismatch := buildTarGz(t, []tarEntry{{name: "unexpected.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")}})
	sum = sha256.Sum256(mismatch)
	header = &agentv1.ReceiveArtifactHeader{
		TransferId:    "malicious",
		Root:          RootVolumes,
		Directory:     ".",
		Length:        uint64(len(mismatch)),
		Sha256:        hex.EncodeToString(sum[:]),
		ExpandedBytes: 1,
		EntryCount:    1,
		TopLevel:      []string{"expected.txt"},
	}
	stream = &scriptedReceiveStream{msgs: receiveMessages(header, mismatch)}
	if err := destination.ReceiveArtifact(stream); err != nil {
		t.Fatalf("ReceiveArtifact: %v", err)
	}
	if _, err := applyArtifact(destination, "malicious", stream.resp, ".", header.TopLevel, context.Background()); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("top-level mismatch: code = %v, want FailedPrecondition (%v)", status.Code(err), err)
	}
	assertNoTransferStaging(t, destDir)
}

func TestApplyArtifactCancellationKeepsPublishedTopLevels(t *testing.T) {
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", "a")
	writeTreeFile(t, srcDir, "b.txt", "b")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "cancel-publish", "a.txt", "b.txt")
	body := readArtifact(t, source, "cancel-publish", prep.GetArtifact())

	destDir := t.TempDir()
	destination := newTransferServer(t, destDir)
	received := receiveArtifact(t, destination, "cancel-publish", prep, body)

	ctx, cancel := context.WithCancel(context.Background())
	fileTransferPublishHook = func(name string) {
		if name == "b.txt" {
			cancel()
		}
	}
	t.Cleanup(func() { fileTransferPublishHook = nil })
	if _, err := applyArtifact(destination, "cancel-publish", received, ".", prep.GetTopLevel(), ctx); status.Code(err) != codes.Canceled {
		t.Fatalf("apply: code = %v, want Canceled (%v)", status.Code(err), err)
	}
	if got, err := os.ReadFile(filepath.Join(destDir, "a.txt")); err != nil || string(got) != "a" {
		t.Fatalf("published entry was removed: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "b.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unpublished entry was left behind: %v", err)
	}
	assertNoTransferStaging(t, destDir)
}

func TestApplyArtifactCancellationDuringExtract(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", "a")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "cancel-extract", "a.txt")
	body := readArtifact(t, source, "cancel-extract", prep.GetArtifact())

	destDir := t.TempDir()
	destination := newTransferServer(t, destDir)
	received := receiveArtifact(t, destination, "cancel-extract", prep, body)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := applyArtifact(destination, "cancel-extract", received, ".", prep.GetTopLevel(), ctx); status.Code(err) != codes.Canceled {
		t.Fatalf("apply: code = %v, want Canceled (%v)", status.Code(err), err)
	}
	for _, entry := range mustReadDir(t, destDir) {
		if entry.Name() != fileTransferDir {
			t.Fatalf("cancelled extract published %q", entry.Name())
		}
	}
	assertNoTransferStaging(t, destDir)
}

func TestPrepareArtifactCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTreeFile(t, dir, "a.txt", "a")
	server := newTransferServer(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.PrepareBundle(ctx, &agentv1.PrepareBundleRequest{
		TransferId: "cancel-prepare", Root: RootVolumes, Sources: []string{"a.txt"},
	}); status.Code(err) != codes.Canceled {
		t.Fatalf("prepare: code = %v, want Canceled (%v)", status.Code(err), err)
	}
	if entries, err := os.ReadDir(server.transferRoot); err == nil && len(entries) != 0 {
		t.Fatalf("cancelled prepare leaked staging: %v", entries)
	}
}

func TestArtifactExpiryReclaimsOnlyUnusedStaging(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTreeFile(t, dir, "a.txt", "a")
	writeTreeFile(t, dir, "b.txt", "b")
	server := newTransferServer(t, dir)
	stale := prepareBundle(t, server, "expiry", "a.txt")
	active := prepareBundle(t, server, "expiry", "b.txt")

	pinned, err := server.acquireArtifact("expiry", active.GetArtifact())
	if err != nil {
		t.Fatal(err)
	}
	server.artifactsMu.Lock()
	server.artifacts["expiry"][stale.GetArtifact()].expires = time.Now().Add(-time.Minute)
	pinned.expires = time.Now().Add(-time.Minute)
	server.artifactsMu.Unlock()

	server.sweepTransfers(time.Now())

	server.artifactsMu.Lock()
	_, stalePresent := server.artifacts["expiry"][stale.GetArtifact()]
	_, activePresent := server.artifacts["expiry"][active.GetArtifact()]
	server.artifactsMu.Unlock()
	if stalePresent {
		t.Fatal("the expired artifact was not reclaimed")
	}
	if !activePresent {
		t.Fatal("the in-use artifact was swept")
	}
	server.releaseArtifact(pinned)
	if body := readArtifact(t, server, "expiry", active.GetArtifact()); len(body) == 0 {
		t.Fatal("the active artifact became unreadable")
	}
	if err := server.ReadArtifact(&agentv1.ReadArtifactRequest{
		TransferId: "expiry", Artifact: stale.GetArtifact(),
	}, &collectArtifactStream{}); status.Code(err) != codes.NotFound {
		t.Fatalf("expired read: code = %v, want NotFound", status.Code(err))
	}
}

func TestTransferStagingIsHiddenFromTheFileAPI(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", "a")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "hidden", "a.txt")
	body := readArtifact(t, source, "hidden", prep.GetArtifact())

	destDir := t.TempDir()
	destination := newTransferServer(t, destDir)
	received := receiveArtifact(t, destination, "hidden", prep, body)
	if received.GetArtifact() == "" {
		t.Fatal("receive returned no artifact")
	}
	entries, err := destination.List(context.Background(), &agentv1.ListRequest{Root: RootVolumes, Path: "."})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, entry := range entries.GetEntries() {
		if entry.GetName() == fileTransferDir {
			t.Fatal("List exposed the transfer staging directory")
		}
	}
	for _, path := range []string{fileTransferDir, fileTransferDir + "/x", fileTransferDir + "/x/bundle.tar.gz"} {
		if _, err := destination.Stat(context.Background(), &agentv1.StatRequest{Root: RootVolumes, Path: path}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("Stat(%q): code = %v, want InvalidArgument", path, status.Code(err))
		}
		if err := destination.Read(&agentv1.ReadRequest{Root: RootVolumes, Path: path}, &collectReadStream{}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("Read(%q): code = %v, want InvalidArgument", path, status.Code(err))
		}
	}
	if err := destination.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: fileTransferDir}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Delete: code = %v, want InvalidArgument", status.Code(err))
	}
	if err := os.Symlink(fileTransferDir, filepath.Join(destDir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Stat(context.Background(), &agentv1.StatRequest{Root: RootVolumes, Path: "alias/x"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("alias Stat: code = %v, want InvalidArgument", status.Code(err))
	}
	if err := os.Remove(filepath.Join(destDir, "alias")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyArtifactRefusesWrongBinding(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", "a")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "binding", "a.txt")
	body := readArtifact(t, source, "binding", prep.GetArtifact())

	destDir := t.TempDir()
	destination := newTransferServer(t, destDir)
	if err := os.Mkdir(filepath.Join(destDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	received := receiveArtifact(t, destination, "binding", prep, body)
	if _, err := applyArtifact(destination, "binding", received, "sub", prep.GetTopLevel(), context.Background()); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("wrong directory: code = %v, want InvalidArgument", status.Code(err))
	}
	if _, err := applyArtifact(destination, "binding", received, ".", []string{"other.txt"}, context.Background()); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("wrong selection: code = %v, want InvalidArgument", status.Code(err))
	}
	stream, err := applyArtifact(destination, "binding", received, ".", prep.GetTopLevel(), context.Background())
	if err != nil {
		t.Fatalf("ApplyArtifact after refused bindings: %v", err)
	}
	if stream.outcomes()["a.txt"] != agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_PUBLISHED {
		t.Fatalf("outcomes = %v", stream.outcomes())
	}
}

func TestReceiveArtifactRefusesFullDisk(t *testing.T) {
	srcDir := t.TempDir()
	writeTreeFile(t, srcDir, "a.txt", "a")
	source := newTransferServer(t, srcDir)
	prep := prepareBundle(t, source, "disk", "a.txt")
	body := readArtifact(t, source, "disk", prep.GetArtifact())

	old := fileDiskUsage
	fileDiskUsage = func(string) (uint64, uint64) { return 0, 1000 }
	defer func() { fileDiskUsage = old }()

	destination := newTransferServer(t, t.TempDir())
	stream := &scriptedReceiveStream{msgs: receiveMessages(receiveHeader("disk", prep, body), body)}
	if err := destination.ReceiveArtifact(stream); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("full disk: code = %v, want ResourceExhausted (%v)", status.Code(err), err)
	}
}

func TestTransferSourceStageSweep(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTreeFile(t, dir, "a.txt", "a")
	server := newTransferServer(t, dir)
	stage, err := server.newSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * fileTempSweepAge)
	if err := os.Chtimes(stage, old, old); err != nil {
		t.Fatal(err)
	}
	server.sweepTransfers(time.Now())
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned source staging was not reclaimed: %v", err)
	}
}
