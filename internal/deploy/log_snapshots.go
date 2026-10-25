package deploy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const snapshotDeadline = 15 * time.Second

type LogOptions struct {
	Tail     int    `json:"tail"`
	MaxBytes int    `json:"max_bytes"`
	Service  string `json:"service,omitempty"`
	Replica  string `json:"replica,omitempty"`
}

func (o LogOptions) normalized() (LogOptions, error) {
	if o.Tail == 0 {
		o.Tail = 100
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 64 << 10
	}
	if o.Tail < 1 || o.Tail > 1000 || o.MaxBytes < 1 || o.MaxBytes > 256<<10 || len(o.Service) > 128 || len(o.Replica) > 10 {
		return o, ErrValidation
	}
	if o.Replica != "" {
		n, err := strconv.Atoi(o.Replica)
		if err != nil || n < 1 || strconv.Itoa(n) != o.Replica || o.Service == "" {
			return o, ErrValidation
		}
	}
	return o, nil
}

type LogRead struct {
	Log       string `json:"log"`
	Complete  bool   `json:"complete"`
	Truncated *bool  `json:"truncated"`
	Error     string `json:"error,omitempty"`
}
type ContainerLog struct {
	RuntimeContainer
	LogRead
}
type ApplicationLogs struct {
	ApplicationID      uuid.UUID      `json:"application_id"`
	Source             string         `json:"source"`
	Containers         []ContainerLog `json:"containers"`
	SelectionTruncated bool           `json:"selection_truncated"`
	Complete           bool           `json:"complete"`
	Partial            bool           `json:"partial"`
	Error              string         `json:"error,omitempty"`
}
type DeploymentLogs struct {
	ApplicationID   uuid.UUID `json:"application_id"`
	DeploymentID    uuid.UUID `json:"deployment_id"`
	Source          string    `json:"source"`
	Availability    string    `json:"availability"`
	PriorTruncation string    `json:"prior_truncation"`
	LogRead
}

type logSnapshotNode interface {
	StreamLogs(context.Context, *agentv1.StreamLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error)
}

func (n *agentNode) StreamLogs(ctx context.Context, req *agentv1.StreamLogsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return n.docker.StreamLogs(ctx, req, opts...)
}

type chunkReader struct {
	stream grpc.ServerStreamingClient[agentv1.LogChunk]
	data   []byte
}

func (r *chunkReader) Read(p []byte) (int, error) {
	for len(r.data) == 0 {
		chunk, err := r.stream.Recv()
		if err != nil {
			return 0, err
		}
		r.data = chunk.GetData()
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func snapshotError(err error) string {
	switch {
	case errors.Is(err, context.Canceled), status.Code(err) == codes.Canceled:
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), status.Code(err) == codes.DeadlineExceeded:
		return "deadline_exceeded"
	case status.Code(err) == codes.ResourceExhausted:
		return "upstream_too_large"
	case status.Code(err) == codes.NotFound:
		return "container_missing_or_stale"
	default:
		return "agent_unavailable"
	}
}

func readLogPrefix(reader io.Reader, maxBytes, maxLines int) (LogRead, int) {
	out := LogRead{}
	r := bufio.NewReaderSize(reader, 4096)
	var b strings.Builder
	lines, lineStart := 0, true
	for b.Len() < maxBytes && (lines < maxLines || !lineStart) {
		ch, _, err := r.ReadRune()
		if err != nil {
			if err == io.EOF {
				out.Complete = true
				out.Truncated = new(bool)
			} else {
				out.Error = snapshotError(err)
			}
			out.Log = b.String()
			return out, lines
		}
		if b.Len()+utf8.RuneLen(ch) > maxBytes {
			out.Truncated = new(true)
			break
		}
		if lineStart {
			lines++
			lineStart = false
		}
		b.WriteRune(ch)
		if ch == '\n' {
			lineStart = true
		}
	}
	extra := r.Buffered() > 0
	if chunks, ok := reader.(*chunkReader); ok {
		extra = extra || len(chunks.data) > 0
	}
	if extra {
		out.Truncated = new(true)
	}
	out.Log = b.String()
	return out, lines
}

var logCredentialField = regexp.MustCompile(`(?i)((?:password|passwd|token|api[_-]?key|secret|authorization)["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*(?:"|$)|'(?:\\.|[^'\\])*(?:'|$)|(?:(?:basic|bearer)\s+)?[^\s,"'}]+)`)
var logBearer = regexp.MustCompile(`(?i)(bearer\s+)[^\s,"'}]+`)
var logURLCredential = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/]*[:@][^\s]*`)

func maskLog(raw string, env map[string]string, incomplete bool, maxBytes int) string {
	maskValues := make(map[string]string, len(env))
	for key, value := range env {
		if len(value) >= 6 {
			maskValues[key] = value
		}
	}
	if incomplete {
		cut := 0
		for _, value := range maskValues {
			for _, candidate := range []string{value, strings.ReplaceAll(value, "$", "$$")} {
				for n := min(len(candidate), len(raw)); n > 0; n-- {
					if strings.HasSuffix(raw, candidate[:n]) {
						cut = max(cut, n)
						break
					}
				}
			}
		}
		if cut > 0 {
			raw = raw[:len(raw)-cut] + "<redacted>"
		}
	}
	raw = services.Redact(raw, maskValues)
	raw = redactCloneError(raw)
	raw = logURLCredential.ReplaceAllString(raw, "${1}<redacted>")
	raw = logBearer.ReplaceAllString(raw, "${1}<redacted>")
	raw = logCredentialField.ReplaceAllStringFunc(raw, func(field string) string {
		indexes := logCredentialField.FindStringSubmatchIndex(field)
		prefix, value := field[:indexes[3]], field[indexes[3]:]
		if value[0] == '"' || value[0] == '\'' {
			return prefix + value[:1] + "<redacted>" + value[:1]
		}
		return prefix + "<redacted>"
	})
	if len(raw) > maxBytes {
		raw = raw[:maxBytes]
		for !utf8.ValidString(raw) {
			raw = raw[:len(raw)-1]
		}
	}
	return raw
}

func (s *Service) GetApplicationLogs(ctx context.Context, userID, appID uuid.UUID, options LogOptions) (ApplicationLogs, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotDeadline)
	defer cancel()
	options, err := options.normalized()
	if err != nil {
		return ApplicationLogs{}, err
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return ApplicationLogs{}, err
	}
	out := ApplicationLogs{ApplicationID: appID, Source: "runtime", Containers: []ContainerLog{}, Complete: true}
	if !Enabled() {
		return ApplicationLogs{}, ErrDisabled
	}
	if app.ServerID == uuid.Nil {
		out.Complete = false
		out.Partial = true
		out.Error = "no_node_assigned"
		return out, nil
	}
	node, err := s.dialNode(ctx, app.ServerID)
	if err != nil {
		out.Complete = false
		out.Partial = true
		out.Error = "agent_unavailable"
		return out, nil
	}
	defer func() { _ = node.Close() }()
	labels := map[string]string{labelAppID: app.ID.String()}
	if isComposeApp(app) {
		labels = map[string]string{composeProjectLabel: services.ProjectName(app.ID)}
	}
	inventory, err := node.ContainersByLabel(ctx, labels, true, false)
	if err != nil {
		out.Complete = false
		out.Partial = true
		out.Error = snapshotError(err)
		return out, nil
	}
	selected, limited := selectRuntimeContainers(app, inventory)
	out.SelectionTruncated = limited
	for _, c := range selected {
		service, replica := "", ""
		if isComposeApp(app) {
			service = c.GetLabels()[composeServiceLabel]
			replica = c.GetLabels()["com.docker.compose.container-number"]
		}
		if options.Service != "" && service != options.Service || options.Replica != "" && replica != options.Replica {
			continue
		}
		if len(out.Containers) == 10 {
			out.SelectionTruncated = true
			break
		}
		row := RuntimeContainer{ID: c.GetId(), Service: service, Replica: replica, State: c.GetState(), Health: c.GetHealth()}
		out.Containers = append(out.Containers, ContainerLog{RuntimeContainer: row})
	}
	if len(out.Containers) == 0 && (options.Service != "" || options.Replica != "") {
		return ApplicationLogs{}, ErrNotFound
	}
	current, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return ApplicationLogs{}, err
	}
	if current.ServerID != app.ServerID || current.EnvironmentID != app.EnvironmentID || isComposeApp(current) != isComposeApp(app) {
		return ApplicationLogs{}, ErrConflict
	}
	env, err := s.loadComposeEnv(ctx, app, 0)
	if err != nil {
		return ApplicationLogs{}, errors.New("deploy: log masking unavailable")
	}
	if len(out.Containers) == 0 {
		out.Complete = !out.SelectionTruncated
		out.Partial = out.SelectionTruncated
		return out, nil
	}
	streamer, ok := node.(logSnapshotNode)
	if !ok {
		out.Complete = false
		out.Partial = true
		out.Error = "logs_unsupported"
		return out, nil
	}
	bytesLeft, linesLeft := options.MaxBytes, options.Tail
	for i := range out.Containers {
		row := &out.Containers[i]
		if bytesLeft <= 0 || linesLeft <= 0 {
			out.Complete = false
			row.Error = "aggregate_cap"
			continue
		}
		callCtx, closeStream := context.WithCancel(ctx)
		stream, openErr := streamer.StreamLogs(callCtx, &agentv1.StreamLogsRequest{ContainerId: row.ID, Follow: false, Tail: int64(options.Tail)})
		usedLines := 0
		if openErr != nil {
			row.Error = snapshotError(openErr)
		} else {
			row.LogRead, usedLines = readLogPrefix(&chunkReader{stream: stream}, bytesLeft, linesLeft)
			_ = stream.CloseSend()
		}
		closeStream()
		rawBytes := len(row.Log)
		row.Log = maskLog(row.Log, env, !row.Complete, bytesLeft)
		bytesLeft -= max(rawBytes, len(row.Log))
		linesLeft -= usedLines
		if !row.Complete {
			out.Complete = false
			if row.Error == "" {
				bytesLeft = 0
			}
		}
		if row.Error != "" {
			out.Partial = true
		}
	}
	if out.SelectionTruncated {
		out.Complete = false
	}
	out.Partial = out.Partial || !out.Complete
	return out, nil
}

func (s *Service) GetDeploymentLogs(ctx context.Context, userID, appID, deploymentID uuid.UUID, options LogOptions) (DeploymentLogs, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotDeadline)
	defer cancel()
	options, err := options.normalized()
	if err != nil || options.Service != "" || options.Replica != "" || deploymentID == uuid.Nil {
		return DeploymentLogs{}, ErrValidation
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return DeploymentLogs{}, err
	}
	stored, state, more, err := s.repo.GetBoundedBuildLog(ctx, appID, deploymentID, options)
	if err != nil {
		return DeploymentLogs{}, err
	}
	env, err := s.loadComposeEnv(ctx, app, 0)
	if err != nil {
		return DeploymentLogs{}, errors.New("deploy: log masking unavailable")
	}
	read, _ := readLogPrefix(strings.NewReader(stored), options.MaxBytes, options.Tail)
	if more {
		read.Complete = false
		read.Truncated = new(true)
	}
	if !more && len(stored) <= options.MaxBytes {
		read.Complete = true
		read.Truncated = new(false)
	}
	read.Log = maskLog(read.Log, env, !read.Complete, options.MaxBytes)
	out := DeploymentLogs{ApplicationID: appID, DeploymentID: deploymentID, Source: "persisted_build", Availability: "available", PriorTruncation: "unknown", LogRead: read}
	if stored == "" {
		out.Availability = "not_yet_persisted"
		if state == StateRunning || state == StateFailed {
			out.Availability = "no_persisted_output_available"
		}
	}
	return out, nil
}

func logOptionsParam(w http.ResponseWriter, r *http.Request, selectors bool) (LogOptions, bool) {
	options := LogOptions{}
	for key, values := range r.URL.Query() {
		if len(values) != 1 {
			writeJSON(w, 400, errorBody{Message: "invalid log options"})
			return options, false
		}
		switch key {
		case "tail", "max_bytes":
			n, err := strconv.Atoi(values[0])
			if err != nil || n <= 0 {
				writeJSON(w, 400, errorBody{Message: "invalid log limits"})
				return options, false
			}
			if key == "tail" {
				options.Tail = n
			} else {
				options.MaxBytes = n
			}
		case "service":
			if !selectors {
				writeJSON(w, 400, errorBody{Message: "invalid log selector"})
				return options, false
			}
			options.Service = values[0]
		case "replica":
			if !selectors {
				writeJSON(w, 400, errorBody{Message: "invalid log selector"})
				return options, false
			}
			options.Replica = values[0]
		default:
			writeJSON(w, 400, errorBody{Message: "unsupported log option"})
			return options, false
		}
	}
	options, err := options.normalized()
	if err != nil {
		writeJSON(w, 400, errorBody{Message: "invalid log options"})
		return options, false
	}
	return options, true
}
func (h *handler) getApplicationLogs(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	options, ok := logOptionsParam(w, r, true)
	if !ok {
		return
	}
	out, err := h.svc.GetApplicationLogs(r.Context(), userID, appID, options)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
