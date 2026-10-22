package mcp

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/servers"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const observationDeadline = 15 * time.Second
const staleAfter = 30 * time.Second

// Node deliberately excludes SSH credentials, fingerprints and credential flags.
// LastSeen is the source of heartbeat data; CollectedAt never replaces it.
type Node struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	Status                 string     `json:"status"`
	OS                     *string    `json:"os"`
	Arch                   *string    `json:"arch"`
	DockerVersion          *string    `json:"docker_version"`
	AgentVersion           *string    `json:"agent_version"`
	AgentVersionReportedAt *time.Time `json:"agent_version_reported_at"`
	TotalMem               *int64     `json:"total_mem"`
	TotalDisk              *int64     `json:"total_disk"`
	CPUUsage               *float64   `json:"cpu_usage"`
	MemUsage               *float64   `json:"mem_usage"`
	DiskUsage              *float64   `json:"disk_usage"`
	ContainerCount         *int64     `json:"container_count"`
	LastSeen               *time.Time `json:"last_seen"`
	Stale                  *bool      `json:"stale"`
}

type observationUnits struct {
	Usage          string `json:"usage"`
	Capacity       string `json:"capacity"`
	Rate           string `json:"rate"`
	ContainerCount string `json:"container_count"`
}

var nodeUnits = observationUnits{Usage: "fraction", Capacity: "bytes", Rate: "bytes/second", ContainerCount: "count (series: average)"}

type NodesInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum nodes, default 100, range 1..100"`
	Offset int `json:"offset,omitempty" jsonschema:"Offset into the current ID-sorted snapshot; pagination does not reduce upstream work"`
}

type NodesOutput struct {
	Nodes             []Node           `json:"nodes"`
	Total             int              `json:"total"`
	Truncated         bool             `json:"truncated"`
	NextOffset        *int             `json:"next_offset"`
	CollectedAt       time.Time        `json:"collected_at"`
	Source            string           `json:"source"`
	StaleAfterSeconds int              `json:"stale_after_seconds"`
	Units             observationUnits `json:"units"`
}

type NodeInput struct {
	NodeID string `json:"node_id" jsonschema:"Managed server UUID returned by nodes_list, not the internal agent node ID"`
}

type NodeOutput struct {
	Node              Node             `json:"node"`
	CollectedAt       time.Time        `json:"collected_at"`
	Source            string           `json:"source"`
	StaleAfterSeconds int              `json:"stale_after_seconds"`
	Units             observationUnits `json:"units"`
}

type SystemOutput struct {
	Health       *Health      `json:"health"`
	HealthError  *Error       `json:"health_error"`
	Version      *string      `json:"version"`
	VersionError *Error       `json:"version_error"`
	Nodes        *NodesOutput `json:"nodes"`
	NodesError   *Error       `json:"nodes_error"`
	CollectedAt  time.Time    `json:"collected_at"`
	Partial      bool         `json:"partial"`
}

type MetricsInput struct {
	NodeID string    `json:"node_id" jsonschema:"Managed server UUID"`
	From   time.Time `json:"from" jsonschema:"Inclusive RFC3339 range start"`
	To     time.Time `json:"to" jsonschema:"Exclusive RFC3339 range end"`
	Step   string    `json:"step,omitempty" jsonschema:"1m (max 24h), 1h (max 30d), or 1d (max 30d); default 1m"`
}

type MetricPoint struct {
	Bucket         time.Time `json:"bucket"`
	CPUUsage       *float64  `json:"cpu_usage"`
	MemUsage       *float64  `json:"mem_usage"`
	DiskUsage      *float64  `json:"disk_usage"`
	NetRxBps       *float64  `json:"net_rx_bps"`
	NetTxBps       *float64  `json:"net_tx_bps"`
	DiskReadBps    *float64  `json:"disk_read_bps"`
	DiskWriteBps   *float64  `json:"disk_write_bps"`
	ContainerCount *float64  `json:"container_count"`
}

type MetricsOutput struct {
	NodeID      string           `json:"node_id"`
	From        time.Time        `json:"from"`
	To          time.Time        `json:"to"`
	Step        string           `json:"step"`
	Points      []MetricPoint    `json:"points"`
	State       string           `json:"state"`
	Error       *Error           `json:"error"`
	CollectedAt time.Time        `json:"collected_at"`
	Source      string           `json:"source"`
	Units       observationUnits `json:"units"`
}

func registerObservationTools(server *sdk.Server, client *Client) {
	annotation := &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	sdk.AddTool(server, &sdk.Tool{
		Name: "system_overview", Annotations: annotation,
		Description: "Dependency health, control-plane version and at most 100 visible nodes. " +
			"Partial probes are explicit; this does not establish application or routing readiness.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, SystemOutput, error) {
		out, err := client.systemOverview(ctx)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "nodes_list", Annotations: annotation,
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				"offset": map[string]any{"type": "integer", "minimum": 0},
			},
		},
		Description: "Bounded ID-sorted snapshot of active-team and legacy shared nodes. " +
			"Visibility does not establish team ownership. No SSH metadata.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input NodesInput) (*sdk.CallToolResult, NodesOutput, error) {
		out, err := client.nodesList(ctx, input)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "node_overview", Annotations: annotation,
		Description: "Authorized stored node heartbeat snapshot, nullable agent version and " +
			"explicit staleness. No live reachability probe.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input NodeInput) (*sdk.CallToolResult, NodeOutput, error) {
		out, err := client.nodeOverview(ctx, input)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "node_metrics", Annotations: annotation,
		Description: "Bounded historical heartbeat metrics. Missing buckets remain gaps and " +
			"nulls remain unknown; 404 cannot distinguish disabled, unsupported or missing.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input MetricsInput) (*sdk.CallToolResult, MetricsOutput, error) {
		out, err := client.nodeMetrics(ctx, input)
		return nil, out, err
	})
}

func markStale(node *Node, now time.Time) {
	node.Stale = nil
	if node.LastSeen != nil {
		stale := now.Sub(*node.LastSeen) > staleAfter
		node.Stale = &stale
	}
	if node.Status == "offline" {
		stale := true
		node.Stale = &stale
	}
}

func (c *Client) nodesList(ctx context.Context, input NodesInput) (NodesOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, observationDeadline)
	defer cancel()
	if input.Limit == 0 {
		input.Limit = 100
	}
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return NodesOutput{}, &Error{Code: "invalid_request"}
	}
	var data struct {
		Servers []Node `json:"servers"`
	}
	if err := c.readJSON(ctx, "/api/v1/servers", &data); err != nil {
		return NodesOutput{}, err
	}
	if data.Servers == nil {
		return NodesOutput{}, &Error{Code: "invalid_response"}
	}
	now := time.Now().UTC()
	sort.Slice(data.Servers, func(i, j int) bool { return data.Servers[i].ID < data.Servers[j].ID })
	start := min(input.Offset, len(data.Servers))
	end := start + min(input.Limit, len(data.Servers)-start)
	out := NodesOutput{
		Nodes: make([]Node, 0, end-start), Total: len(data.Servers),
		CollectedAt: now, Source: "stored_heartbeat", Units: nodeUnits,
		StaleAfterSeconds: int(staleAfter.Seconds()), Truncated: end < len(data.Servers),
	}
	for _, node := range data.Servers[start:end] {
		if node.ID == "" || node.Status == "" {
			return NodesOutput{}, &Error{Code: "invalid_response"}
		}
		markStale(&node, now)
		out.Nodes = append(out.Nodes, node)
	}
	if out.Truncated {
		out.NextOffset = &end
	}
	return out, nil
}

func serverPath(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil {
		return "", &Error{Code: "invalid_request"}
	}
	return "/api/v1/servers/" + url.PathEscape(parsed.String()), nil
}

func (c *Client) nodeOverview(ctx context.Context, input NodeInput) (NodeOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, observationDeadline)
	defer cancel()
	path, err := serverPath(input.NodeID)
	if err != nil {
		return NodeOutput{}, err
	}
	var data struct {
		Server Node `json:"server"`
	}
	if err := c.readJSON(ctx, path, &data); err != nil {
		return NodeOutput{}, err
	}
	if data.Server.ID == "" || path != "/api/v1/servers/"+data.Server.ID || data.Server.Status == "" {
		return NodeOutput{}, &Error{Code: "invalid_response"}
	}
	now := time.Now().UTC()
	markStale(&data.Server, now)
	return NodeOutput{
		Node: data.Server, CollectedAt: now, Source: "stored_heartbeat",
		StaleAfterSeconds: int(staleAfter.Seconds()), Units: nodeUnits,
	}, nil
}

// observationError preserves local categories and status. Authorization and
// contract failures must never be softened into a partially successful tool.
func observationError(err error) (*Error, error) {
	if err == nil {
		return nil, nil
	}
	var e *Error
	if !errors.As(err, &e) {
		return nil, &Error{Code: "upstream_unavailable"}
	}
	switch e.Code {
	case "unauthorized", "forbidden", "contract_mismatch", "cancelled", "deadline_exceeded":
		return nil, e
	}
	return e, nil
}

func (c *Client) systemOverview(ctx context.Context) (SystemOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, observationDeadline)
	defer cancel()
	// Public health and instance version alone cannot establish team authority.
	if _, err := c.Context(ctx); err != nil {
		return SystemOutput{}, err
	}
	out := SystemOutput{CollectedAt: time.Now().UTC()}
	health, err := c.Health(ctx)
	if err == nil && (health.Status == "" || health.DB == "" || health.Redis == "") {
		err = &Error{Code: "invalid_response"}
	}
	if out.HealthError, err = observationError(err); err != nil {
		return SystemOutput{}, err
	}
	if out.HealthError == nil {
		out.Health = &health
	}
	var version struct {
		Version string `json:"version"`
	}
	err = c.readJSON(ctx, "/api/v1/version", &version)
	if err == nil && version.Version == "" {
		err = &Error{Code: "invalid_response"}
	}
	if out.VersionError, err = observationError(err); err != nil {
		return SystemOutput{}, err
	}
	if out.VersionError == nil {
		out.Version = &version.Version
	}
	nodes, err := c.nodesList(ctx, NodesInput{})
	if out.NodesError, err = observationError(err); err != nil {
		return SystemOutput{}, err
	}
	if out.NodesError == nil {
		out.Nodes = &nodes
	}
	out.Partial = out.HealthError != nil || out.VersionError != nil || out.NodesError != nil
	return out, nil
}

func (c *Client) nodeMetrics(ctx context.Context, input MetricsInput) (MetricsOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, observationDeadline)
	defer cancel()
	path, err := serverPath(input.NodeID)
	if err != nil {
		return MetricsOutput{}, err
	}
	if input.Step == "" {
		input.Step = "1m"
	}
	bucket, err := servers.ParseMetricsQuery(input.From, input.To, input.Step)
	if err != nil {
		return MetricsOutput{}, &Error{Code: "invalid_request"}
	}
	query := url.Values{"from": {input.From.Format(time.RFC3339Nano)}, "to": {input.To.Format(time.RFC3339Nano)}, "step": {input.Step}}
	var data struct {
		Step   string        `json:"step"`
		Points []MetricPoint `json:"points"`
	}
	err = c.readJSON(ctx, path+"/metrics?"+query.Encode(), &data)
	out := MetricsOutput{
		NodeID: input.NodeID, From: input.From, To: input.To, Step: input.Step,
		CollectedAt: time.Now().UTC(), Source: "persisted_heartbeat",
		Units: nodeUnits, Points: make([]MetricPoint, 0),
	}
	if out.Error, err = observationError(err); err != nil {
		return MetricsOutput{}, err
	}
	if out.Error != nil {
		out.State = "unavailable"
		if out.Error.Status == 404 {
			out.State = "unsupported_or_disabled"
		}
		return out, nil
	}
	// Unaligned ranges may include one more epoch-aligned bucket.
	maxPoints := int((input.To.Sub(input.From.Truncate(bucket)) + bucket - 1) / bucket)
	if data.Step != input.Step || data.Points == nil || len(data.Points) > maxPoints {
		return MetricsOutput{}, &Error{Code: "invalid_response"}
	}
	for i, point := range data.Points {
		if point.Bucket.IsZero() || !point.Bucket.Equal(point.Bucket.Truncate(bucket)) ||
			point.Bucket.Before(input.From.Truncate(bucket)) || !point.Bucket.Before(input.To) ||
			(i > 0 && !point.Bucket.After(data.Points[i-1].Bucket)) {
			return MetricsOutput{}, &Error{Code: "invalid_response"}
		}
	}
	out.Points = append(out.Points, data.Points...)
	out.State = "available"
	if len(out.Points) == 0 {
		out.State = "no_samples"
	}
	return out, nil
}
