package files

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestTailBytes(t *testing.T) {
	tests := []struct {
		name   string
		window string
		want   string
	}{
		{"drops partial first line", "artial\nfirst\nsecond\n", "first\nsecond\n"},
		{"drops the first whole line", "a\nb\nc\n", "b\nc\n"},
		{"no newline keeps the line", "one-long-line", "one-long-line"},
		{"one over-long line ending in a newline", "one-long-line\n", "one-long-line"},
		{"empty", "", ""},
		{"only a newline", "\n", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := string(tailBytes([]byte(test.window))); got != test.want {
				t.Fatalf("tailBytes(%q) = %q, want %q", test.window, got, test.want)
			}
		})
	}
}

func TestTailBytesKeepsLastLines(t *testing.T) {
	var body strings.Builder
	for i := 0; i < TailLines+10; i++ {
		body.WriteString("line\n")
	}
	got := string(tailBytes([]byte(body.String())))
	if strings.Count(got, "\n") != TailLines {
		t.Fatalf("tail has %d lines, want %d", strings.Count(got, "\n"), TailLines)
	}
	if !strings.HasSuffix(got, "line\n") {
		t.Fatalf("tail does not end at the file end: %q", got)
	}
}

func TestTailBytesNeverSplitsMultiByte(t *testing.T) {
	window := []byte("a\u00e9b")[2:]
	got := string(tailBytes(window))
	if got != "b" {
		t.Fatalf("tailBytes = %q, want %q", got, "b")
	}
}

func TestContentTailBoundary(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	ctx := scopeCtx("", projectID)

	tests := []struct {
		name      string
		body      string
		truncated bool
		want      string
	}{
		{name: "limit-1", body: "abcd\nef", want: "abcd\nef"},
		{name: "limit", body: "abcd\nefg", want: "abcd\nefg"},
		{name: "limit+1", body: "abcd\nefgh", truncated: true, want: "efgh"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeFileClient{
				statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{
					Name: "f", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: int64(len(test.body)), Etag: `"v1"`,
				}},
				readBody: []byte(test.body),
			}
			svc := newTestService(t, registry, client)
			content, err := svc.Content(ctx, server.ID, "root", "f")
			if err != nil {
				t.Fatalf("content: %v", err)
			}
			if content.Truncated != test.truncated {
				t.Fatalf("truncated = %v, want %v", content.Truncated, test.truncated)
			}
			if content.Content != test.want {
				t.Fatalf("content = %q, want %q", content.Content, test.want)
			}
			if content.Size != int64(len(test.body)) {
				t.Fatalf("size = %d, want %d", content.Size, len(test.body))
			}
			if !test.truncated {
				return
			}
			if req := client.seenRead; req.GetOffset() != 1 || req.GetLimit() != int64(8) {
				t.Fatalf("read range = offset %d limit %d, want 1/8", req.GetOffset(), req.GetLimit())
			}
		})
	}
}

func TestContentTailDecodesLegacyEncoding(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	line := gbkFixture(t, "配置已保存\n")
	body := bytes.Repeat(line, 4)
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{
			Name: "app.conf", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: int64(len(body)),
		}},
		readBody: body,
	}
	svc := newTestService(t, registry, client)
	svc.maxContentBytes = 17

	content, err := svc.Content(scopeCtx("", projectID), server.ID, "root", "app.conf")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if !content.Truncated {
		t.Fatal("a body over the ceiling must be truncated")
	}
	if content.Encoding != EncodingGB2312 || content.Content != "配置已保存\n" {
		t.Fatalf("content = %q (%s), want one clean GB2312 line", content.Content, content.Encoding)
	}
	if strings.ContainsRune(content.Content, '\uFFFD') {
		t.Fatal("the cut split a multi-byte character")
	}
}

func TestContentTailRefusesShrunkFile(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{
			Name: "f", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 9,
		}},
	}
	svc := newTestService(t, registry, client)

	if _, err := svc.Content(scopeCtx("", projectID), server.ID, "root", "f"); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestEditorWriteToOversizeFileRefused(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	ctx := scopeCtx("", projectID)
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "f", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 9, Etag: `"v1"`}},
		writeResp: writeEntry(),
	}
	svc := newTestService(t, registry, client)

	_, err := svc.Write(ctx, server.ID, WriteRequest{
		Root: "volumes", Path: "f", ExpectEtag: `"v1"`, Size: 2, FromEditor: true,
	}, strings.NewReader("hi"))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("editor write err = %v, want ErrTooLarge", err)
	}
	if client.lastWrite != nil {
		t.Fatal("a refused editor save must not reach the agent")
	}

	if _, err := svc.Write(ctx, server.ID, WriteRequest{
		Root: "volumes", Path: "f", ExpectEtag: `"v1"`, Size: 2,
	}, strings.NewReader("hi")); err != nil {
		t.Fatalf("non-editor write err = %v, want nil", err)
	}
}

func TestEditorWriteAtCeilingAllowed(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "f", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 8, Etag: `"v1"`}},
		writeResp: writeEntry(),
	}
	svc := newTestService(t, registry, client)

	if _, err := svc.Write(scopeCtx("", projectID), server.ID, WriteRequest{
		Root: "volumes", Path: "f", ExpectEtag: `"v1"`, Size: 2, FromEditor: true,
	}, strings.NewReader("hi")); err != nil {
		t.Fatalf("write at the ceiling: %v", err)
	}
}

func TestTruncatedContentIsNeverSnapshotted(t *testing.T) {
	history := &fakeHistory{}
	svc := NewService(Config{Registry: newFakeRegistry(), HistoryMaxVersions: 10, HistoryMaxBytes: 1024})
	svc.SetHistoryStore(history)

	_, err := svc.saveVersionDetached(context.Background(), uuid.New(), "volumes", "f", Content{
		Content: "tail", Etag: `"v1"`, Truncated: true, raw: []byte("tail"),
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if history.savedCount() != 0 {
		t.Fatalf("saved %d versions for a truncated view, want 0", history.savedCount())
	}
}

func TestPutContentRejectsOversizeFile(t *testing.T) {
	projectID := uuid.New()
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "f", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: MaxContentBytes + 1, Etag: `"v1"`}},
		writeResp: writeEntry(),
	}
	router, server := newRealRouter(t, "", projectID, client)

	rec := doWrite(t, router, http.MethodPut,
		"/v1/servers/"+server.ID.String()+"/files/content?root=volumes&path=f",
		"hi", map[string]string{"If-Match": `"v1"`})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"too_large"`) {
		t.Fatalf("body = %s, want the too_large token", rec.Body.String())
	}
	if client.lastWrite != nil {
		t.Fatal("a refused save must not reach the agent")
	}
}
