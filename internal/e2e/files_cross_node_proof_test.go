package e2e

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/files"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// This file is the cross-node proof for the split-view copy feature. It drives
// a real control-plane API against two real agent processes, each with isolated
// roots and its own agent-owned transfer staging directory.

const proofSourceRoot = "source-archive"
const proofDestinationRoot = "dest-archive"

type proofNodes struct {
	harness    *transferHarness
	sourceRoot string // read-only root on the source node
	destRoot   string // read-only root on the destination node
}

func newProofNodes(t *testing.T, ctx context.Context, options transferHarnessOptions) *proofNodes {
	t.Helper()
	sourceRoot := filepath.Join(canonicalTempDir(t), proofSourceRoot)
	destRoot := filepath.Join(canonicalTempDir(t), proofDestinationRoot)
	for _, dir := range []string{sourceRoot, destRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	options.sourceConfig.FileRoots = sourceRoot
	options.destinationConfig.FileRoots = destRoot
	return &proofNodes{
		harness:    newTransferHarnessWith(t, ctx, options),
		sourceRoot: sourceRoot,
		destRoot:   destRoot,
	}
}

func proofBody(t *testing.T, sources []string, destPath string, destinationID uuid.UUID) string {
	t.Helper()
	payload := map[string]any{
		"source_root":      proofSourceRoot,
		"source_directory": "",
		"sources":          sources,
		"destination": map[string]any{
			"server_id": destinationID.String(),
			"root":      "volumes",
			"path":      destPath,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode transfer body: %v", err)
	}
	return string(encoded)
}

func planTransferProof(t *testing.T, h *transferHarness, body string) files.TransferPlan {
	t.Helper()
	status, payload := h.request(t, "/v1/servers/"+h.sourceID.String()+"/files/transfers/plan", body)
	if status != http.StatusOK {
		t.Fatalf("plan = %d (%s), want 200", status, payload)
	}
	var plan files.TransferPlan
	if err := json.Unmarshal([]byte(payload), &plan); err != nil {
		t.Fatalf("decode plan: %v (%s)", err, payload)
	}
	return plan
}

func proofRoots(t *testing.T, h *transferHarness, serverID uuid.UUID) map[string]bool {
	t.Helper()
	status, payload := h.do(t, http.MethodGet, "/v1/servers/"+serverID.String()+"/files/roots?root=", "")
	if status != http.StatusOK {
		t.Fatalf("roots = %d (%s), want 200", status, payload)
	}
	var decoded struct {
		Roots []files.Root `json:"roots"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode roots: %v (%s)", err, payload)
	}
	roots := make(map[string]bool, len(decoded.Roots))
	for _, root := range decoded.Roots {
		roots[root.Name] = root.Writable
	}
	return roots
}

func writeProofFile(t *testing.T, root, rel, content string, mode os.FileMode) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	if err := os.Chmod(full, mode); err != nil {
		t.Fatalf("chmod %s: %v", rel, err)
	}
}

type proofEntry struct {
	digest string
	mode   os.FileMode
	target string
	dir    bool
}

func snapshotTree(t *testing.T, root string) map[string]proofEntry {
	t.Helper()
	snapshot := make(map[string]proofEntry)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := proofEntry{mode: info.Mode().Perm(), dir: entry.IsDir()}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			item.target = target
		case entry.Type().IsRegular():
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			item.digest = hex.EncodeToString(sum[:])
		}
		snapshot[rel] = item
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}

func assertTreesEqual(t *testing.T, label string, before, after map[string]proofEntry) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("%s entry count changed: %d -> %d", label, len(before), len(after))
	}
	for rel, want := range before {
		got, ok := after[rel]
		if !ok {
			t.Fatalf("%s entry %q disappeared", label, rel)
		}
		if got != want {
			t.Fatalf("%s entry %q changed: %+v -> %+v", label, rel, want, got)
		}
	}
}

func digestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func assertNoTransferStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".gotham-transfer"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read staging in %s: %v", root, err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging was not cleaned in %s: %v", root, entries)
	}
}

func TestFilesCrossNodeCopyProof(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodes := newProofNodes(t, ctx, transferHarnessOptions{})
	h := nodes.harness

	sourceRoots := proofRoots(t, h, h.sourceID)
	if sourceRoots[proofSourceRoot] {
		t.Fatalf("source root %q is writable, want read-only", proofSourceRoot)
	}
	destRoots := proofRoots(t, h, h.destinationID)
	if destRoots[proofDestinationRoot] {
		t.Fatalf("destination root %q is writable, want read-only", proofDestinationRoot)
	}
	if _, ok := sourceRoots[proofDestinationRoot]; ok {
		t.Fatalf("source node exposes the destination root name %q", proofDestinationRoot)
	}
	if _, ok := destRoots[proofSourceRoot]; ok {
		t.Fatalf("destination node exposes the source root name %q", proofSourceRoot)
	}

	writeProofFile(t, nodes.sourceRoot, "regular.txt", "regular copy proof\n", 0o640)
	writeProofFile(t, nodes.sourceRoot, "empty.txt", "", 0o644)
	writeProofFile(t, nodes.sourceRoot, "nested/a.txt", "alpha\n", 0o644)
	writeProofFile(t, nodes.sourceRoot, "nested/unicode-π-数据.txt", "unicode\n", 0o600)
	if err := os.MkdirAll(filepath.Join(nodes.sourceRoot, "nested", "empty-dir"), 0o755); err != nil {
		t.Fatalf("mkdir empty-dir: %v", err)
	}
	if err := os.Symlink("regular.txt", filepath.Join(nodes.sourceRoot, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	before := snapshotTree(t, nodes.sourceRoot)

	sources := []string{"regular.txt", "empty.txt", "nested", "link.txt"}
	body := proofBody(t, sources, "", h.destinationID)
	plan := planTransferProof(t, h, body)
	if plan.Mode != files.TransferModeBundle {
		t.Fatalf("plan mode = %q, want %q", plan.Mode, files.TransferModeBundle)
	}
	if !plan.Bounds.BundleAvailable || plan.Bounds.MaxSources == 0 || plan.Bounds.MaxEntries == 0 {
		t.Fatalf("plan bounds = %+v, want negotiated bundle limits", plan.Bounds)
	}
	if len(plan.Unsupported) != 1 || plan.Unsupported[0].Name != "link.txt" {
		t.Fatalf("plan unsupported = %+v, want link.txt", plan.Unsupported)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("plan conflicts = %v, want none", plan.Conflicts)
	}
	assertTreesEqual(t, "source after plan", before, snapshotTree(t, nodes.sourceRoot))
	if entries, err := os.ReadDir(nodes.harness.destRoot); err != nil || len(entries) != 0 {
		t.Fatalf("plan wrote to the destination: %v (err %v)", entries, err)
	}

	events := h.streamTransfer(t, body)
	summary := transferSummary(t, events)
	if summary.Mode != files.TransferModeBundle || summary.Copied != 3 || summary.Skipped != 1 || summary.Failed != 0 {
		t.Fatalf("summary = %+v, want 3 copied and 1 skipped", summary)
	}
	for _, name := range []string{"regular.txt", "empty.txt", "nested"} {
		if result := transferResultByName(t, events, name); result.Status != files.TransferStatusCopied {
			t.Fatalf("result %q = %+v, want copied", name, result)
		}
	}
	if result := transferResultByName(t, events, "link.txt"); result.Status != files.TransferStatusSkipped {
		t.Fatalf("symlink result = %+v, want skipped", result)
	}

	destRegular := filepath.Join(h.destRoot, "regular.txt")
	if got := digestFile(t, destRegular); got != before["regular.txt"].digest {
		t.Fatalf("regular.txt digest = %s, want %s", got, before["regular.txt"].digest)
	}
	if info, err := os.Stat(destRegular); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("regular.txt mode = %v, err %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(h.destRoot, "empty.txt")); err != nil || info.Size() != 0 {
		t.Fatalf("empty.txt = %+v, err %v", info, err)
	}
	for rel, name := range map[string]string{
		"nested/a.txt":            "alpha\n",
		"nested/unicode-π-数据.txt": "unicode\n",
	} {
		body, err := os.ReadFile(filepath.Join(h.destRoot, rel))
		if err != nil || string(body) != name {
			t.Fatalf("%s = %q, err %v", rel, body, err)
		}
	}
	if info, err := os.Stat(filepath.Join(h.destRoot, "nested", "empty-dir")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(h.destRoot, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("symlink was published: %v", err)
	}
	assertTreesEqual(t, "source after copy", before, snapshotTree(t, nodes.sourceRoot))
	assertNoTransferStaging(t, h.destRoot)

	// A single file is a direct write and therefore lands with the node's
	// default file mode; only a bundle carries per-entry metadata.
	if err := os.MkdirAll(filepath.Join(h.destRoot, "direct"), 0o755); err != nil {
		t.Fatalf("mkdir direct: %v", err)
	}
	directBody := proofBody(t, []string{"regular.txt"}, "direct", h.destinationID)
	directPlan := planTransferProof(t, h, directBody)
	if directPlan.Mode != files.TransferModeDirect {
		t.Fatalf("direct plan mode = %q", directPlan.Mode)
	}
	directEvents := h.streamTransfer(t, directBody)
	if summary := transferSummary(t, directEvents); summary.Copied != 1 {
		t.Fatalf("direct summary = %+v", summary)
	}
	directPath := filepath.Join(h.destRoot, "direct", "regular.txt")
	if got := digestFile(t, directPath); got != before["regular.txt"].digest {
		t.Fatalf("direct copy digest = %s", got)
	}
	if info, err := os.Stat(directPath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("direct copy mode = %v, err %v, want the 0644 default", info, err)
	}

	// The same-node local copy still works inside one root.
	status, payload := h.request(t, "/v1/servers/"+h.destinationID.String()+"/files/copy",
		`{"root":"volumes","source":"regular.txt","destination":"regular-local.txt"}`)
	if status != http.StatusOK {
		t.Fatalf("local copy = %d (%s), want 200", status, payload)
	}
	if got := digestFile(t, filepath.Join(h.destRoot, "regular-local.txt")); got != before["regular.txt"].digest {
		t.Fatalf("local copy digest = %s", got)
	}

	// A read-only root refuses an ordinary write from the control plane.
	if status, payload := h.do(t, http.MethodPut,
		"/v1/servers/"+h.sourceID.String()+"/files/content?root="+proofSourceRoot+"&path=blocked.txt&overwrite=true",
		"no"); status != http.StatusForbidden {
		t.Fatalf("write into the read-only source root = %d (%s), want 403", status, payload)
	}
}

func TestFilesCrossNodeConflictProof(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodes := newProofNodes(t, ctx, transferHarnessOptions{})
	h := nodes.harness

	writeProofFile(t, nodes.sourceRoot, "regular.txt", "source version\n", 0o644)
	writeProofFile(t, nodes.sourceRoot, "tree/new.txt", "new\n", 0o644)
	writeProofFile(t, nodes.sourceRoot, "raced.txt", "raced source\n", 0o644)
	writeProofFile(t, h.destRoot, "regular.txt", "destination original\n", 0o644)
	writeProofFile(t, h.destRoot, "tree/keep.txt", "keep\n", 0o644)

	body := proofBody(t, []string{"regular.txt", "tree"}, "", h.destinationID)
	plan := planTransferProof(t, h, body)
	if len(plan.Conflicts) != 2 || plan.Conflicts[0] != "regular.txt" || plan.Conflicts[1] != "tree" {
		t.Fatalf("known plan conflicts = %v", plan.Conflicts)
	}

	events := h.streamTransfer(t, body)
	summary := transferSummary(t, events)
	if summary.Conflicts != 2 || summary.Copied != 0 {
		t.Fatalf("known conflict summary = %+v", summary)
	}
	for _, name := range []string{"regular.txt", "tree"} {
		if result := transferResultByName(t, events, name); result.Status != files.TransferStatusConflict {
			t.Fatalf("result %q = %+v, want conflict", name, result)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(h.destRoot, "regular.txt")); string(got) != "destination original\n" {
		t.Fatalf("conflicting file was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(h.destRoot, "tree", "keep.txt")); string(got) != "keep\n" {
		t.Fatalf("existing destination tree changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.destRoot, "tree", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("conflicting directory was merged: %v", err)
	}

	// Raced conflict: the name is free at plan time and appears before the transfer.
	racedBody := proofBody(t, []string{"raced.txt"}, "", h.destinationID)
	racedPlan := planTransferProof(t, h, racedBody)
	if len(racedPlan.Conflicts) != 0 {
		t.Fatalf("raced plan conflicts = %v, want none yet", racedPlan.Conflicts)
	}
	writeProofFile(t, h.destRoot, "raced.txt", "raced destination\n", 0o644)
	racedEvents := h.streamTransfer(t, racedBody)
	if summary := transferSummary(t, racedEvents); summary.Conflicts != 1 || summary.Copied != 0 {
		t.Fatalf("raced summary = %+v", summary)
	}
	if result := transferResultByName(t, racedEvents, "raced.txt"); result.Status != files.TransferStatusConflict {
		t.Fatalf("raced result = %+v", result)
	}
	if got, _ := os.ReadFile(filepath.Join(h.destRoot, "raced.txt")); string(got) != "raced destination\n" {
		t.Fatalf("raced destination was overwritten: %q", got)
	}
	assertNoTransferStaging(t, h.destRoot)
}

func TestFilesCrossNodeSkipProof(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodes := newProofNodes(t, ctx, transferHarnessOptions{})
	h := nodes.harness

	if err := os.Symlink("missing", filepath.Join(nodes.sourceRoot, "only-link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	body := proofBody(t, []string{"only-link"}, "", h.destinationID)
	plan := planTransferProof(t, h, body)
	if len(plan.Unsupported) != 1 || len(plan.Conflicts) != 0 {
		t.Fatalf("all-skipped plan = %+v", plan)
	}
	events := h.streamTransfer(t, body)
	summary := transferSummary(t, events)
	// Nothing transferable is refused while preparing: the entry is a failure
	// with an explicit reason, never a silent empty success.
	if summary.Copied != 0 || summary.Skipped != 0 || summary.Failed != 1 || summary.Reason != "" {
		t.Fatalf("all-skipped summary = %+v", summary)
	}
	if result := transferResultByName(t, events, "only-link"); result.Status != files.TransferStatusFailed || result.Reason != "invalid" {
		t.Fatalf("all-skipped result = %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(h.destRoot, "only-link")); !os.IsNotExist(err) {
		t.Fatalf("all-skipped selection published something: %v", err)
	}
	assertNoTransferStaging(t, h.destRoot)
}

func TestFilesCrossNodeSingleFileLimitProof(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodes := newProofNodes(t, ctx, transferHarnessOptions{
		serviceConfig: files.Config{MaxUploadBytes: 1 << 20},
	})
	h := nodes.harness

	writeProofFile(t, nodes.sourceRoot, "big.bin", "0123456789abcdef", 0o644)
	if err := os.Truncate(filepath.Join(nodes.sourceRoot, "big.bin"), 1<<20+1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	body := proofBody(t, []string{"big.bin"}, "", h.destinationID)
	status, payload := h.request(t, "/v1/servers/"+h.sourceID.String()+"/files/transfers", body)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("too-large plan = %d (%s), want 413", status, payload)
	}
	if !strings.Contains(payload, "too large") {
		t.Fatalf("too-large payload lacks an explicit reason: %s", payload)
	}
	if _, err := os.Stat(filepath.Join(h.destRoot, "big.bin")); !os.IsNotExist(err) {
		t.Fatalf("too-large file was written: %v", err)
	}
}

func TestFilesCrossNodeCancelProof(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sourceID uuid.UUID
	nodes := newProofNodes(t, ctx, transferHarnessOptions{
		dialWrap: func(id uuid.UUID, client files.FileClient) files.FileClient {
			if id == sourceID {
				return &blockingPrepareClient{FileClient: client}
			}
			return client
		},
	})
	h := nodes.harness
	sourceID = h.sourceID

	writeProofFile(t, nodes.sourceRoot, "data/a.txt", "alpha\n", 0o644)
	before := snapshotTree(t, nodes.sourceRoot)
	body := proofBody(t, []string{"data"}, "", h.destinationID)

	requestCtx, stop := context.WithCancel(context.Background())
	defer stop()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost,
		h.baseURL+"/v1/servers/"+h.sourceID.String()+"/files/transfers", strings.NewReader(body))
	if err != nil {
		t.Fatalf("cancel request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("start transfer: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("transfer = %d (%s), want 200", response.StatusCode, payload)
	}

	reader := bufio.NewScanner(response.Body)
	reader.Buffer(make([]byte, 64<<10), 1<<20)
	if !reader.Scan() {
		t.Fatalf("no first event: %v", reader.Err())
	}
	var first files.TransferEvent
	if err := json.Unmarshal(reader.Bytes(), &first); err != nil {
		t.Fatalf("first event = %q: %v", reader.Text(), err)
	}
	if first.Type != files.TransferEventPhase || first.Phase != files.TransferPhasePreparing {
		t.Fatalf("first event = %+v, want preparing", first)
	}
	stop()
	for reader.Scan() {
		if strings.Contains(reader.Text(), `"type":"summary"`) {
			t.Fatalf("a cancelled stream delivered a terminal summary: %s", reader.Text())
		}
	}
	if err := reader.Err(); err != nil && !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("read cancelled stream: %v", err)
	}

	assertTreesEqual(t, "source after cancel", before, snapshotTree(t, nodes.sourceRoot))
	entries, err := os.ReadDir(h.destRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read destination: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancel published destination entries: %v", entries)
	}
	assertNoTransferStaging(t, h.destRoot)
}

func TestFilesCrossNodeLostAcknowledgmentProof(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var destinationID uuid.UUID
	applyStream := &lostAckClient{}
	nodes := newProofNodes(t, ctx, transferHarnessOptions{
		dialWrap: func(id uuid.UUID, client files.FileClient) files.FileClient {
			if id == destinationID {
				applyStream.FileClient = client
				return applyStream
			}
			return client
		},
	})
	h := nodes.harness
	destinationID = h.destinationID

	writeProofFile(t, nodes.sourceRoot, "data/a.txt", "alpha\n", 0o644)
	body := proofBody(t, []string{"data"}, "", h.destinationID)
	events := h.streamTransfer(t, body)
	summary := transferSummary(t, events)
	if summary.Status != files.TransferStatusUncertain || summary.Uncertain != 1 {
		t.Fatalf("lost acknowledgement summary = %+v, want one uncertain entry", summary)
	}
	if result := transferResultByName(t, events, "data"); result.Status != files.TransferStatusUncertain {
		t.Fatalf("lost acknowledgement result = %+v", result)
	}
	if applyStream.calls != 1 {
		t.Fatalf("ApplyArtifact calls = %d, want exactly one (no automatic replay)", applyStream.calls)
	}
	if _, err := os.Stat(filepath.Join(h.destRoot, "data")); !os.IsNotExist(err) {
		t.Fatalf("uncertain entry was published: %v", err)
	}
	assertNoTransferStaging(t, h.destRoot)
}

// blockingPrepareClient stalls bundle preparation until the request context is
// cancelled, so a client cancel always lands during the preparation phase.
type blockingPrepareClient struct {
	files.FileClient
}

func (c *blockingPrepareClient) PrepareBundle(ctx context.Context, _ *agentv1.PrepareBundleRequest, _ ...grpc.CallOption) (*agentv1.PrepareBundleResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// lostAckClient reports that publication started and then ends the stream
// without an outcome, which is a lost publication acknowledgement.
type lostAckClient struct {
	files.FileClient
	calls int
}

func (c *lostAckClient) ApplyArtifact(_ context.Context, _ *agentv1.ApplyArtifactRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ApplyArtifactEvent], error) {
	c.calls++
	return &lostAckStream{events: []*agentv1.ApplyArtifactEvent{{
		Event: &agentv1.ApplyArtifactEvent_Phase{Phase: &agentv1.TransferPhase{Phase: "publish", Path: "data"}},
	}}}, nil
}

type lostAckStream struct {
	grpc.ClientStream
	events []*agentv1.ApplyArtifactEvent
}

func (s *lostAckStream) Recv() (*agentv1.ApplyArtifactEvent, error) {
	if len(s.events) == 0 {
		return nil, io.EOF
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}
