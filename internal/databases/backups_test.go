package databases

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// jobFrame renders a successful raw frame the way a temporary container could
// write it: engine chatter first, then the markers and the payload.
func jobFrame(runID string, payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("engine: temporary server started\n")
	buf.WriteString(jobStartPrefix + runID + "\n")
	buf.WriteString(jobPayloadPrefix + runID + " " + strconv.Itoa(len(payload)) + "\n")
	buf.Write(payload)
	buf.WriteString("\n" + jobEndPrefix + runID + " ok\n")
	buf.WriteString("engine: shutdown complete\n")
	return buf.Bytes()
}

// jobFrameB64 renders a successful dump frame the way the engine scripts
// write it: the payload base64-encoded, followed directly by the end marker
// (the real scripts emit no newline between the two), plus post-frame engine
// chatter the collector must keep as diagnostics.
func jobFrameB64(runID string, payload []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(payload)
	var buf bytes.Buffer
	buf.WriteString("engine: temporary server started\n")
	buf.WriteString(jobStartPrefix + runID + "\n")
	buf.WriteString(jobPayloadB64Prefix + runID + " " + strconv.Itoa(len(encoded)) + "\n")
	buf.WriteString(encoded)
	buf.WriteString(jobEndPrefix + runID + " ok\n")
	buf.WriteString("engine: shutdown complete\n")
	return buf.Bytes()
}

// splitEvery cuts a buffer into fixed-size pieces so the collector is exercised
// across arbitrary chunk boundaries, the way Docker splits log frames.
func splitEvery(data []byte, size int) [][]byte {
	var chunks [][]byte
	for len(data) > 0 {
		if len(data) <= size {
			chunks = append(chunks, data)
			break
		}
		chunks = append(chunks, data[:size])
		data = data[size:]
	}
	return chunks
}

func TestBackupEngineDumpOptions(t *testing.T) {
	tests := []struct {
		engine    string
		version   string
		image     string
		mountPath string
		tool      string
		envKey    string
	}{
		{EnginePostgres, "", "postgres:16-alpine", "/var/lib/postgresql/data", "pg_dump", "POSTGRES_USER"},
		{EngineMySQL, "8.4", "mysql:8.4", "/var/lib/mysql", "mysqldump", "MYSQL_ROOT_PASSWORD"},
		{EngineMariaDB, "", "mariadb:11.4", "/var/lib/mysql", "mysqldump", "MYSQL_ROOT_PASSWORD"},
		{EngineMongoDB, "", "mongo:7.0", "/data/db", "mongodump", "MONGO_INITDB_ROOT_USERNAME"},
		{EngineRedis, "", "redis:7.2-alpine", "/data", "tar -cf", "REDIS_PASSWORD"},
	}
	for _, test := range tests {
		t.Run(test.engine, func(t *testing.T) {
			engine, ok := LookupBackupEngine(test.engine)
			if !ok {
				t.Fatalf("no backup engine for %q", test.engine)
			}
			database := testDatabase(test.engine, test.version)
			options, err := engine.DumpOptions(database, testCredentials(), "run-1")
			if err != nil {
				t.Fatalf("DumpOptions: %v", err)
			}
			if options.Image != test.image {
				t.Errorf("image = %q, want %q", options.Image, test.image)
			}
			if len(options.Command) != 3 || options.Command[0] != "sh" || options.Command[1] != "-c" {
				t.Fatalf("command = %v, want sh -c <script>", options.Command)
			}
			if !strings.Contains(options.Command[2], test.tool) {
				t.Errorf("script does not run %q", test.tool)
			}
			if !strings.Contains(options.Command[2], jobStartPrefix) {
				t.Errorf("script does not emit the frame markers")
			}
			wantVolume := database.StoragePath + ":" + test.mountPath
			if len(options.Volumes) != 1 || options.Volumes[0] != wantVolume {
				t.Errorf("volumes = %v, want [%s]", options.Volumes, wantVolume)
			}
			if !containsEnv(options.Env, "GOTHAM_RUN_ID=run-1") {
				t.Errorf("env misses GOTHAM_RUN_ID: %v", options.Env)
			}
			if !containsEnv(options.Env, test.envKey+"=") {
				t.Errorf("env misses %s: %v", test.envKey, options.Env)
			}
			if options.Labels[labelRole] != roleBackup {
				t.Errorf("role label = %q, want %q", options.Labels[labelRole], roleBackup)
			}
			if options.Labels[labelBackupID] != "run-1" {
				t.Errorf("backup id label = %q", options.Labels[labelBackupID])
			}
			if !strings.HasPrefix(options.Name, "gotham-backup-") {
				t.Errorf("container name = %q, want gotham-backup-*", options.Name)
			}
			if len(options.Ports) != 0 || len(options.Networks) != 0 {
				t.Errorf("job container must publish and join nothing: %v %v", options.Ports, options.Networks)
			}
		})
	}
}

func TestBackupEngineRestoreOptions(t *testing.T) {
	tests := []struct {
		engine string
		tool   string
		staged string
	}{
		{EnginePostgres, "pg_restore", "/var/lib/postgresql/data/.gotham-restore/x.part"},
		{EngineMySQL, "mysql -h", "/var/lib/mysql/.gotham-restore/x.part"},
		{EngineMongoDB, "mongorestore", "/data/db/.gotham-restore/x.part"},
		{EngineRedis, "tar -xf", "/data/.gotham-restore/x.part"},
	}
	for _, test := range tests {
		t.Run(test.engine, func(t *testing.T) {
			engine, ok := LookupBackupEngine(test.engine)
			if !ok {
				t.Fatalf("no backup engine for %q", test.engine)
			}
			options, err := engine.RestoreOptions(testDatabase(test.engine, ""), testCredentials(), "run-2", test.staged)
			if err != nil {
				t.Fatalf("RestoreOptions: %v", err)
			}
			if !strings.Contains(options.Command[2], test.tool) {
				t.Errorf("script does not run %q", test.tool)
			}
			if !containsEnv(options.Env, "GOTHAM_STAGED="+test.staged) {
				t.Errorf("env misses GOTHAM_STAGED: %v", options.Env)
			}
			if options.Labels[labelRole] != roleRestore {
				t.Errorf("role label = %q, want %q", options.Labels[labelRole], roleRestore)
			}
		})
	}
}

// TestRestoreScriptsValidateDecompression pins the restore contract shared by
// all five engines: the staged artifact is decompressed with a checked status
// before the engine tool runs, the decompressor is never read through a
// pipeline whose status could mask corruption, and a decompression failure
// ends the frame with status 5.
func TestRestoreScriptsValidateDecompression(t *testing.T) {
	for _, name := range engineOrder {
		t.Run(name, func(t *testing.T) {
			engine, ok := LookupBackupEngine(name)
			if !ok {
				t.Fatalf("no backup engine for %q", name)
			}
			options, err := engine.RestoreOptions(testDatabase(name, ""), testCredentials(),
				"run-2", "/var/lib/postgresql/data/.gotham-restore/x.part")
			if err != nil {
				t.Fatalf("RestoreOptions: %v", err)
			}
			script := options.Command[2]
			if !strings.Contains(script, `gunzip -c "$payload" >"$archive"`) {
				t.Errorf("%s: restore does not decompress with a checked status:\n%s", name, script)
			}
			if !strings.Contains(script, "status=5") {
				t.Errorf("%s: a decompression failure has no distinct exit status", name)
			}
			if strings.Contains(script, `gunzip -c "$payload" 2>>"$log" |`) {
				t.Errorf("%s: the decompressor status is discarded by a pipeline", name)
			}
		})
	}
}

// TestPostgresRestoreUsesFileAndTransaction pins the PostgreSQL restore
// invocation: pg_restore reads the checked decompressed archive by name (a
// trailing "-" is a file name, not stdin) and runs inside a single
// transaction, so a mid-archive failure cannot leave partial changes.
func TestPostgresRestoreUsesFileAndTransaction(t *testing.T) {
	engine, _ := LookupBackupEngine(EnginePostgres)
	options, err := engine.RestoreOptions(testDatabase(EnginePostgres, ""), testCredentials(),
		"run-2", "/var/lib/postgresql/data/.gotham-restore/x.part")
	if err != nil {
		t.Fatalf("RestoreOptions: %v", err)
	}
	script := options.Command[2]
	if !strings.Contains(script, "pg_restore") {
		t.Fatalf("script does not run pg_restore: %s", script)
	}
	if !strings.Contains(script, `--no-owner --single-transaction "$archive"`) {
		t.Errorf("pg_restore must read the checked archive inside one transaction:\n%s", script)
	}
}

func TestBackupJobOptionsValidation(t *testing.T) {
	engine, _ := LookupBackupEngine(EnginePostgres)
	database := testDatabase(EnginePostgres, "")

	withoutVolume := database
	withoutVolume.StoragePath = "some-volume"
	if _, err := engine.DumpOptions(withoutVolume, testCredentials(), "run-1"); err == nil {
		t.Error("expected an error for a database without a gotham volume")
	}
	if _, err := engine.DumpOptions(database, testCredentials(), ""); err == nil {
		t.Error("expected an error for a job without a run id")
	}
	unknown := database
	unknown.Engine = "oracle"
	if _, err := engine.DumpOptions(unknown, testCredentials(), "run-1"); err == nil {
		t.Error("expected an error for an unsupported engine")
	}
}

func TestStagingPath(t *testing.T) {
	database := testDatabase(EnginePostgres, "")
	dir, err := stagingDir(database)
	if err != nil {
		t.Fatalf("stagingDir: %v", err)
	}
	if dir != "/var/lib/postgresql/data/.gotham-restore" {
		t.Errorf("dir = %q", dir)
	}
	path, err := stagingPath(database, database.ID)
	if err != nil {
		t.Fatalf("stagingPath: %v", err)
	}
	if path != dir+"/"+database.ID.String()+".part" {
		t.Errorf("path = %q", path)
	}
}

func TestTempJobName(t *testing.T) {
	name := tempJobName(roleBackup, "12345678-abcd")
	if name != "gotham-backup-12345678" {
		t.Errorf("name = %q", name)
	}
	long := tempJobName("restore-stage-3", strings.Repeat("a", 80))
	if len(long) > 63 {
		t.Errorf("name is %d characters, want at most 63", len(long))
	}
}

func TestJobCollectorSuccess(t *testing.T) {
	payload := []byte{0x00, 0x01, '\n', 0xff, 'P', 'K'}
	var stored bytes.Buffer
	collector := newJobCollector("run-1", &stored)

	frame := jobFrame("run-1", payload)
	for _, chunk := range splitEvery(frame, 7) {
		if _, err := collector.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := collector.Result(); err != nil {
		t.Fatalf("Result: %v", err)
	}
	if collector.Written() != int64(len(payload)) {
		t.Errorf("written = %d, want %d", collector.Written(), len(payload))
	}
	if !bytes.Equal(stored.Bytes(), payload) {
		t.Errorf("payload = %v, want %v", stored.Bytes(), payload)
	}
}

func TestJobCollectorIgnoresForeignRun(t *testing.T) {
	var stored bytes.Buffer
	collector := newJobCollector("run-2", &stored)
	// Output of an earlier container: its markers must never be mistaken for
	// this run's frame.
	frame := jobFrame("run-1", []byte("stale"))
	if _, err := collector.Write(frame); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := collector.Result(); err == nil {
		t.Fatal("expected an error: the frame belongs to another run")
	}
}

func TestJobCollectorFailureCarriesDiagnostics(t *testing.T) {
	collector := newJobCollector("run-1", nil)
	frame := "GOTHAM-BACKUP-START run-1\nGOTHAM-BACKUP-END run-1 fail 12\npg_dump: error: connection refused\n"
	if _, err := collector.Write([]byte(frame)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	err := collector.Result()
	if err == nil {
		t.Fatal("expected an error for a failed job")
	}
	if !strings.Contains(err.Error(), "status 1") {
		t.Errorf("error = %q, want the exit status", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want the engine diagnostics", err)
	}
}

func TestJobCollectorWithoutMarkerIsBounded(t *testing.T) {
	collector := newJobCollector("run-1", nil)
	// Binary noise with no newline at all: the buffer must not grow without
	// bound, and the collector must still fail cleanly at the end.
	noise := bytes.Repeat([]byte{0x5a, 0xa5}, 64<<10)
	for _, chunk := range splitEvery(noise, 4096) {
		if _, err := collector.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if len(collector.buf) > jobMarkerLimit {
		t.Errorf("internal buffer grew to %d bytes, want at most %d", len(collector.buf), jobMarkerLimit)
	}
	err := collector.Result()
	if err == nil || !strings.Contains(err.Error(), "without a completion marker") {
		t.Fatalf("error = %v, want a missing-marker failure", err)
	}
	if diag := collector.Diagnostics(); strings.Contains(diag, "\x00") {
		t.Error("diagnostics should not carry NUL bytes")
	}
}

func TestJobCollectorAcceptsNoPayloadFrame(t *testing.T) {
	// Restore and staging jobs announce a zero-length payload.
	collector := newJobCollector("run-1", nil)
	frame := "GOTHAM-BACKUP-START run-1\nGOTHAM-BACKUP-PAYLOAD run-1 0\nGOTHAM-BACKUP-END run-1 ok\n"
	for _, chunk := range splitEvery([]byte(frame), 5) {
		if _, err := collector.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := collector.Result(); err != nil {
		t.Fatalf("Result: %v", err)
	}
	if collector.Written() != 0 {
		t.Errorf("written = %d, want 0", collector.Written())
	}
}

func TestJobCollectorBase64Payload(t *testing.T) {
	// 200 KiB of arbitrary bytes stream through in base64: the decoded
	// artifact must match byte for byte.
	payload := make([]byte, 200_000)
	for i := range payload {
		payload[i] = byte(i*7 + 3)
	}
	var stored bytes.Buffer
	collector := newJobCollector("run-1", &stored)
	for _, chunk := range splitEvery(jobFrameB64("run-1", payload), 1021) {
		if _, err := collector.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := collector.Result(); err != nil {
		t.Fatalf("Result: %v", err)
	}
	if collector.Written() != int64(len(payload)) {
		t.Errorf("written = %d, want %d", collector.Written(), len(payload))
	}
	if !bytes.Equal(stored.Bytes(), payload) {
		t.Errorf("stored %d bytes, want %d", stored.Len(), len(payload))
	}
}

func TestJobCollectorBase64AcrossSmallChunks(t *testing.T) {
	// A chunk boundary can fall inside a base64 quantum; every boundary from
	// one to seven bytes exercises the carry of the streaming decoder.
	payload := []byte{0x00, 0x01, 0x02, '\n', 0xff, 0xfe, 'P', 'K', 0x7f}
	for size := 1; size <= 7; size++ {
		var stored bytes.Buffer
		collector := newJobCollector("run-1", &stored)
		for _, chunk := range splitEvery(jobFrameB64("run-1", payload), size) {
			if _, err := collector.Write(chunk); err != nil {
				t.Fatalf("size %d: Write: %v", size, err)
			}
		}
		if err := collector.Result(); err != nil {
			t.Fatalf("size %d: Result: %v", size, err)
		}
		if !bytes.Equal(stored.Bytes(), payload) {
			t.Errorf("size %d: payload = %v, want %v", size, stored.Bytes(), payload)
		}
	}
}

// TestJobCollectorBase64PaddingAcrossWrites covers positive payloads whose
// base64 ends in one or two padding characters, split at every byte boundary:
// validity must not depend on how the log stream chunks the writes.
func TestJobCollectorBase64PaddingAcrossWrites(t *testing.T) {
	for size := 1; size <= 8; size++ {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte('a' + i)
		}
		frame := jobFrameB64("run-1", payload)
		for split := 0; split <= len(frame); split++ {
			var stored bytes.Buffer
			collector := newJobCollector("run-1", &stored)
			if _, err := collector.Write(frame[:split]); err != nil {
				t.Fatalf("size %d split %d: write head: %v", size, split, err)
			}
			if _, err := collector.Write(frame[split:]); err != nil {
				t.Fatalf("size %d split %d: write tail: %v", size, split, err)
			}
			if err := collector.Result(); err != nil {
				t.Fatalf("size %d split %d: Result: %v", size, split, err)
			}
			if !bytes.Equal(stored.Bytes(), payload) {
				t.Fatalf("size %d split %d: got %q, want %q", size, split, stored.Bytes(), payload)
			}
		}
	}
}

// TestJobCollectorRejectsInteriorPadding pins the terminal-padding state: once
// a quantum carries "=", the encoded stream ended, so any later quantum is
// invalid whether or not it arrives in the same Write call.
func TestJobCollectorRejectsInteriorPadding(t *testing.T) {
	head := jobStartPrefix + "run-1\n" + jobPayloadB64Prefix + "run-1 8\n"
	tail := jobEndPrefix + "run-1 ok\n"
	cases := []struct {
		name   string
		writes []string
	}{
		{name: "one write", writes: []string{head + "aA==aQ==" + tail}},
		{name: "split writes", writes: []string{head + "aA==", "aQ==" + tail}},
		{name: "split inside padding", writes: []string{head + "aA=", "=aQ==" + tail}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			collector := newJobCollector("run-1", nil)
			for _, write := range test.writes {
				if _, err := collector.Write([]byte(write)); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}
			if err := collector.Result(); err == nil {
				t.Fatal("expected interior padding to fail the frame")
			}
		})
	}
}

// TestJobCollectorRejectsUnderdeclaredExtraPayload is the review's F2 case: a
// B64 frame that declares fewer bytes than it carries must fail instead of
// silently storing a truncated artifact, whatever the Write boundaries and
// even when the excess looks like a repeated payload header.
func TestJobCollectorRejectsUnderdeclaredExtraPayload(t *testing.T) {
	head := jobStartPrefix + "run-1\n" + jobPayloadB64Prefix + "run-1 4\n"
	end := jobEndPrefix + "run-1 ok\n"
	cases := []struct {
		name   string
		writes []string
	}{
		{name: "one write", writes: []string{head + "YWJjZGVm\n" + end}},
		{name: "payload split", writes: []string{head + "YWJj", "ZGVm\n" + end}},
		{name: "repeated payload header", writes: []string{head + "YWJj", jobPayloadB64Prefix + "run-1 4\n", "ZGVm" + end}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var stored bytes.Buffer
			collector := newJobCollector("run-1", &stored)
			for _, write := range test.writes {
				if _, err := collector.Write([]byte(write)); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}
			if err := collector.Result(); err == nil {
				t.Fatalf("under-declared payload accepted: truncated decoded=%q", stored.String())
			}
		})
	}
}

// TestJobCollectorBase64KeepsPostFrameDiagnostics proves the strict end-marker
// rule stops at the frame: chatter after the end marker is still collected.
func TestJobCollectorBase64KeepsPostFrameDiagnostics(t *testing.T) {
	const chatter = "engine: shutdown complete\n"
	frame := string(jobFrameB64("run-1", []byte("payload")))
	frame = strings.TrimSuffix(frame, chatter)
	collector := newJobCollector("run-1", nil)
	if _, err := collector.Write([]byte(frame)); err != nil {
		t.Fatalf("Write frame: %v", err)
	}
	if _, err := collector.Write([]byte(chatter)); err != nil {
		t.Fatalf("Write chatter: %v", err)
	}
	if err := collector.Result(); err != nil {
		t.Fatalf("Result: %v", err)
	}
	if !strings.Contains(collector.Diagnostics(), "shutdown complete") {
		t.Errorf("post-frame diagnostics were dropped: %q", collector.Diagnostics())
	}
}

func TestJobCollectorRejectsCorruptBase64(t *testing.T) {
	collector := newJobCollector("run-1", nil)
	frame := jobFrameB64("run-1", []byte("payload"))
	frame = bytes.Replace(frame, []byte("cGF5bG9hZA=="), []byte("cGF5bG9hZ!=="), 1)
	if _, err := collector.Write(frame); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := collector.Result(); err == nil {
		t.Fatal("expected corrupt base64 to fail the frame")
	}
}

func TestJobCollectorRejectsPayloadSizeMismatch(t *testing.T) {
	// The declared encoded size is the frame's integrity check: a stream that
	// is shorter or longer than announced must fail instead of producing a
	// truncated artifact.
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	header := jobStartPrefix + "run-1\n" + jobPayloadB64Prefix + "run-1 "
	end := jobEndPrefix + "run-1 ok\n"

	larger := header + strconv.Itoa(len(encoded)+4) + "\n" + encoded + end
	collector := newJobCollector("run-1", nil)
	if _, err := collector.Write([]byte(larger)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := collector.Result(); err == nil {
		t.Fatal("expected a declared size larger than the payload to fail the frame")
	}

	truncated := header + strconv.Itoa(len(encoded)) + "\n" + encoded[:len(encoded)-4] + end
	collector = newJobCollector("run-1", nil)
	if _, err := collector.Write([]byte(truncated)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := collector.Result(); err == nil {
		t.Fatal("expected a truncated payload to fail the frame")
	}
}

func TestBackupEngineDumpScriptsEncodePayload(t *testing.T) {
	for _, name := range engineOrder {
		engine, ok := LookupBackupEngine(name)
		if !ok {
			t.Fatalf("no backup engine for %q", name)
		}
		options, err := engine.DumpOptions(testDatabase(name, ""), testCredentials(), "run-1")
		if err != nil {
			t.Fatalf("%s: DumpOptions: %v", name, err)
		}
		script := options.Command[2]
		if !strings.Contains(script, jobPayloadB64Prefix) {
			t.Errorf("%s: script does not emit the base64 payload marker", name)
		}
		if !strings.Contains(script, "base64 ") {
			t.Errorf("%s: script does not encode the payload with base64", name)
		}
		if strings.Contains(script, `cat "$tmp"`) {
			t.Errorf("%s: script still streams the raw payload", name)
		}
	}
}

func TestBoundedDiag(t *testing.T) {
	long := strings.Repeat("x", jobDiagLimit*2)
	bounded := boundedDiag(long)
	if len(bounded) > jobDiagLimit+utf8.RuneLen('…') {
		t.Errorf("boundedDiag returned %d bytes", len(bounded))
	}
	if !strings.HasSuffix(bounded, strings.Repeat("x", jobDiagLimit)) {
		t.Error("boundedDiag must keep the tail of the diagnostic")
	}
}
