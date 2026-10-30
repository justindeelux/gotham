package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// History categories the cleanup center offers as one aggregate item each.
const (
	HistoryDeploymentLogs   = "deployment_logs"
	HistoryToolchainJobLogs = "toolchain_job_logs"
	HistoryRequestReceipts  = "request_receipts"
	HistoryWebhookEvents    = "webhook_events"
)

// HistoryDeleteBatch bounds one delete statement so a large table never locks
// for long.
const HistoryDeleteBatch = 5000

// HistoryStat is one history category's reclaimable rows and bytes.
type HistoryStat struct {
	Name  string
	Rows  int64
	Bytes int64
}

// CleanupRetentionDays reads the owner's cleanup windows. It is read on every
// scan and clean so a saved setting applies without a restart.
func (s *Store) CleanupRetentionDays(ctx context.Context) (int32, int32, error) {
	row, err := s.queries.GetInstanceSettings(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("cleanup: load retention settings: %w", err)
	}
	return row.CleanupHistoryRetentionDays, row.CleanupBackupFileMinAgeDays, nil
}

// ListBackupLocations reports every artifact a backups row still references.
func (s *Store) ListBackupLocations(ctx context.Context) ([]string, error) {
	return s.queries.BackedUpFileLocations(ctx)
}

// HistoryStats reports the rows older than cutoff in every history category.
func (s *Store) HistoryStats(ctx context.Context, cutoff time.Time) ([]HistoryStat, error) {
	ts := pgtype.Timestamptz{Time: cutoff, Valid: true}
	deployments, err := s.queries.DeploymentHistoryStats(ctx, ts)
	if err != nil {
		return nil, fmt.Errorf("deployment logs history: %w", err)
	}
	toolchain, err := s.queries.ToolchainHistoryStats(ctx, ts)
	if err != nil {
		return nil, fmt.Errorf("toolchain logs history: %w", err)
	}
	receipts, err := s.queries.RequestReceiptHistoryStats(ctx, ts)
	if err != nil {
		return nil, fmt.Errorf("request receipts history: %w", err)
	}
	webhooks, err := s.queries.WebhookHistoryStats(ctx, ts)
	if err != nil {
		return nil, fmt.Errorf("webhook events history: %w", err)
	}
	return []HistoryStat{
		{Name: HistoryDeploymentLogs, Rows: deployments.RowCount, Bytes: deployments.Bytes},
		{Name: HistoryToolchainJobLogs, Rows: toolchain.RowCount, Bytes: toolchain.Bytes},
		{Name: HistoryRequestReceipts, Rows: receipts.RowCount, Bytes: receipts.Bytes},
		{Name: HistoryWebhookEvents, Rows: webhooks.RowCount, Bytes: webhooks.Bytes},
	}, nil
}

// DeleteHistoryBatch removes at most batch rows of one category older than
// cutoff and reports how many it removed.
func (s *Store) DeleteHistoryBatch(ctx context.Context, name string, cutoff time.Time, batch int) (int64, error) {
	ts := pgtype.Timestamptz{Time: cutoff, Valid: true}
	limit := int32(batch)
	switch name {
	case HistoryDeploymentLogs:
		return s.queries.ClearDeploymentHistory(ctx, sqlc.ClearDeploymentHistoryParams{CreatedAt: ts, Limit: limit})
	case HistoryToolchainJobLogs:
		return s.queries.DeleteToolchainHistory(ctx, sqlc.DeleteToolchainHistoryParams{CreatedAt: ts, Limit: limit})
	case HistoryRequestReceipts:
		return s.queries.DeleteRequestReceiptHistory(ctx, sqlc.DeleteRequestReceiptHistoryParams{AcceptedAt: ts, Limit: limit})
	case HistoryWebhookEvents:
		return s.queries.DeleteWebhookHistory(ctx, sqlc.DeleteWebhookHistoryParams{ReceivedAt: ts, Limit: limit})
	default:
		return 0, fmt.Errorf("cleanup: unknown history category %q", name)
	}
}
