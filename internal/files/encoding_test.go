package files

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func gbkFixture(t *testing.T, s string) []byte {
	t.Helper()
	encoded, err := gbkEncoding.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encode GBK fixture %q: %v", s, err)
	}
	return encoded
}

func big5Fixture(t *testing.T, s string) []byte {
	t.Helper()
	encoded, err := big5Encoding.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encode BIG5 fixture %q: %v", s, err)
	}
	return encoded
}

func newEncodingTestService(t *testing.T, registry Registry, client FileClient) *Service {
	t.Helper()
	svc := newTestService(t, registry, client)
	svc.maxContentBytes = 1 << 20
	return svc
}

func TestDetectEncoding(t *testing.T) {
	simplified := "服务器配置文件已经保存成功，请重新启动服务。"
	traditional := "伺服器設定檔已經儲存成功，請重新啟動服務。"
	tests := []struct {
		name     string
		body     []byte
		encoding string
		text     string
	}{
		{name: "utf-8", body: []byte("services:\n  web:\n"), encoding: EncodingUTF8, text: "services:\n  web:\n"},
		{name: "utf-8 bom", body: []byte("\uFEFFname=app\n"), encoding: EncodingUTF8, text: "\uFEFFname=app\n"},
		{name: "empty", body: nil, encoding: EncodingUTF8, text: ""},
		{name: "gb2312", body: gbkFixture(t, simplified), encoding: EncodingGB2312, text: simplified},
		{name: "gbk extension", body: gbkFixture(t, "价格€明细"), encoding: EncodingGBK, text: "价格€明细"},
		{name: "big5", body: big5Fixture(t, traditional), encoding: EncodingBIG5, text: traditional},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodingName, text, err := detectEncoding(test.body)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if encodingName != test.encoding {
				t.Fatalf("encoding = %q, want %q", encodingName, test.encoding)
			}
			if text != test.text {
				t.Fatalf("text = %q, want %q", text, test.text)
			}
			if test.body != nil && encodingName != EncodingUTF8 {
				encoded, err := encodeContent(encodingName, text)
				if err != nil {
					t.Fatalf("re-encode: %v", err)
				}
				if string(encoded) != string(test.body) {
					t.Fatalf("round trip = %x, want %x", encoded, test.body)
				}
			}
		})
	}
}

func TestLegacyFixturesRoundTripByteIdentically(t *testing.T) {
	fixtures := map[string][]byte{
		EncodingGB2312: gbkFixture(t, "服务器配置\t已保存。\r\n\f重启服务，请检查：网络、端口。"),
		EncodingGBK:    gbkFixture(t, "价格€明细\r\n"),
		EncodingBIG5:   big5Fixture(t, "伺服器設定檔\t已經儲存。\r\n請重新啟動服務，並檢查網路設定。"),
	}
	for want, body := range fixtures {
		t.Run(want, func(t *testing.T) {
			encodingName, text, err := detectEncoding(body)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if encodingName != want {
				t.Fatalf("encoding = %q, want %q", encodingName, want)
			}
			encoded, err := encodeContent(encodingName, text)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if string(encoded) != string(body) {
				t.Fatalf("round trip = %x, want %x", encoded, body)
			}
		})
	}
}

func TestDetectEncodingRejectsNonRoundTripBytes(t *testing.T) {
	body := []byte{0xA2, 0xCC}
	encodingName, text, err := detectEncoding(body)
	if errors.Is(err, ErrNotEditable) {
		return
	}
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if encodingName == EncodingBIG5 {
		t.Fatalf("detected big5, whose round trip changes the bytes")
	}
	encoded, err := encodeContent(encodingName, text)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if string(encoded) != string(body) {
		t.Fatalf("round trip = %x, want %x", encoded, body)
	}
}

func TestDetectEncodingRefusesBinary(t *testing.T) {
	for name, body := range map[string][]byte{
		"nul byte":     {'a', 0x00, 'b'},
		"undecodable":  {0x81, 0x20, 0x81, 0x20},
		"png header":   append([]byte("\x89PNG\r\n\x1a\n"), []byte("payload")...),
		"gif header":   []byte("GIF89a payload"),
		"control byte": append(gbkFixture(t, "配置"), 0x01),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := detectEncoding(body); !errors.Is(err, ErrNotEditable) {
				t.Fatalf("err = %v, want ErrNotEditable", err)
			}
		})
	}
}

func TestEncodeContentRefusesUnrepresentable(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		text     string
	}{
		{name: "emoji into gb2312", encoding: EncodingGB2312, text: "中文😀"},
		{name: "emoji into gbk", encoding: EncodingGBK, text: "中文😀"},
		{name: "emoji into big5", encoding: EncodingBIG5, text: "中文😀"},
		{name: "non-ascii into ascii", encoding: EncodingASCII, text: "café"},
		{name: "gbk extension into gb2312", encoding: EncodingGB2312, text: "价格€"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := encodeContent(test.encoding, test.text); !errors.Is(err, ErrUnrepresentable) {
				t.Fatalf("err = %v, want ErrUnrepresentable", err)
			}
		})
	}
}

func TestNormalizeEncoding(t *testing.T) {
	for input, want := range map[string]string{
		"":        EncodingUTF8,
		" UTF-8 ": EncodingUTF8,
		"utf8":    EncodingUTF8,
		"ansi":    EncodingUTF8,
		"ASCII":   EncodingASCII,
		"gbk":     EncodingGBK,
		"gb-2312": EncodingGB2312,
		"GB2312":  EncodingGB2312,
		"big-5":   EncodingBIG5,
	} {
		got, err := normalizeEncoding(input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalize %q = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeEncoding("utf-16"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown encoding err = %v, want ErrValidation", err)
	}
}

func TestContentReportsEncoding(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	body := gbkFixture(t, "配置已保存")
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{
			Name: "app.conf", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: int64(len(body)), Etag: `"v1"`, Mode: 0o644,
		}},
		readBody: body,
	}
	svc := newEncodingTestService(t, registry, client)

	content, err := svc.Content(scopeCtx(teams.RoleOwner, teamID), server.ID, "root", "app.conf")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if content.Encoding != EncodingGB2312 || content.Content != "配置已保存" {
		t.Fatalf("content = %+v", content)
	}
	if string(content.raw) != string(body) {
		t.Fatal("Content must keep the raw body for history")
	}
	if content.Size != int64(len(body)) {
		t.Fatalf("size = %d, want raw %d", content.Size, len(body))
	}
}

func TestWriteTranscodesLegacyEncoding(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newEncodingTestService(t, registry, client)

	text := "配置已保存"
	utf8Body := strings.NewReader(text)
	if _, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "app.conf", ExpectEtag: `"v1"`, Size: int64(len(text)), Encoding: "gbk",
	}, utf8Body); err != nil {
		t.Fatalf("write: %v", err)
	}
	want := gbkFixture(t, text)
	if string(client.lastWriteBody()) != string(want) {
		t.Fatalf("agent body = %x, want %x", client.lastWriteBody(), want)
	}
	if header := client.lastWriteHeader(); header.GetSize() != int64(len(want)) {
		t.Fatalf("header size = %d, want %d", header.GetSize(), len(want))
	}
}

func TestWriteRefusesUnrepresentableRune(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newEncodingTestService(t, registry, client)

	text := "配置😀"
	_, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "app.conf", ExpectEtag: `"v1"`, Size: int64(len(text)), Encoding: "gb2312",
	}, strings.NewReader(text))
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("err = %v, want ErrUnrepresentable", err)
	}
	if client.lastWriteClosed() {
		t.Fatal("a refused transcode must not reach the agent")
	}
}

func TestPutContentTranscodesEncodingQuery(t *testing.T) {
	teamID := uuid.New()
	client := &fakeFileClient{writeResp: writeEntry()}
	handler, server := newRealRouter(t, teams.RoleOwner, teamID, client)

	text := "配置已保存"
	rec := doWrite(t, handler, http.MethodPut,
		"/v1/servers/"+server.ID.String()+"/files/content?root=volumes&path=a.txt&encoding=gbk",
		text, map[string]string{"If-Match": `"v1"`})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	want := gbkFixture(t, text)
	if string(client.lastWriteBody()) != string(want) {
		t.Fatalf("agent body = %x, want %x", client.lastWriteBody(), want)
	}
}

func TestPutContentRefusesUnrepresentableEncoding(t *testing.T) {
	teamID := uuid.New()
	client := &fakeFileClient{writeResp: writeEntry()}
	handler, server := newRealRouter(t, teams.RoleOwner, teamID, client)

	rec := doWrite(t, handler, http.MethodPut,
		"/v1/servers/"+server.ID.String()+"/files/content?root=volumes&path=a.txt&encoding=gb2312",
		"配置😀", map[string]string{"If-Match": `"v1"`})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"unrepresentable"`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHistorySnapshotsRawLegacyBytes(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	old := gbkFixture(t, "旧配置")
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{
			Name: "app.conf", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: int64(len(old)), Etag: `"v1"`, Mode: 0o644,
		}},
		readBody:  old,
		writeResp: writeEntry(),
	}
	svc := newEncodingTestService(t, registry, client)
	history := &fakeHistory{}
	svc.SetHistoryStore(history)

	text := "新配置"
	if _, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "app.conf", ExpectEtag: `"v1"`, Size: int64(len(text)), Encoding: "gbk",
	}, strings.NewReader(text)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(history.saves) != 1 {
		t.Fatalf("saves = %d, want 1", len(history.saves))
	}
	if history.saves[0].content != string(old) {
		t.Fatalf("snapshot = %x, want raw %x", history.saves[0].content, old)
	}
}
