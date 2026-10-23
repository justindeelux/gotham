package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const receiptWhere = `principal_id=$1 AND team_id=$2 AND operation=$3 AND target_id=$4 AND request_key=$5`

func receiptArgs(r *requestreplay.Request) []any {
	return []any{r.Principal, r.Team, r.Operation, r.Target, r.Key}
}

func readReceipt(ctx context.Context, db sqlc.DBTX, r *requestreplay.Request) (*requestreplay.Result, error) {
	result := &requestreplay.Result{Replayed: true, Outcome: "accepted"}
	var fingerprint string
	var expired bool
	err := db.QueryRow(ctx, `SELECT fingerprint, resource_id, related_id, accepted_at, webhook_outcome,
 accepted_at + interval '24 hours' <= clock_timestamp() FROM request_receipts WHERE `+receiptWhere, receiptArgs(r)...).
		Scan(&fingerprint, &result.ResourceID, &result.RelatedID, &result.AcceptedAt, &result.Hook, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Operation != "create_application" {
		result.Hook = ""
	}
	if expired {
		return nil, requestreplay.ErrExpired
	}
	if fingerprint != r.Fingerprint {
		return nil, requestreplay.ErrConflict
	}
	return result, nil
}

func (s *Store) LookupRequestReceipt(ctx context.Context) error {
	r := requestreplay.From(ctx)
	if r == nil {
		return nil
	}
	result, err := readReceipt(ctx, s.DB, r)
	if err == nil {
		r.Result = result
	}
	return err
}

func (s *Store) withRequestReceipt(ctx context.Context, fn func(*sqlc.Queries, *requestreplay.Result) (uuid.UUID, uuid.UUID, error)) (err error) {
	defer func() {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			err = requestreplay.ErrProgress
		}
	}()
	r := requestreplay.From(ctx)
	if r == nil || r.Fingerprint == "" || !requestreplay.ValidKey(r.Key) {
		return fmt.Errorf("replay: incomplete request identity")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		return err
	}
	identity, err := json.Marshal(receiptArgs(r))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(identity)); err != nil {
		return err
	}
	result, err := readReceipt(ctx, tx, r)
	if err != nil {
		return err
	}
	resource, related, err := fn(s.queries.WithTx(tx), result)
	if err != nil {
		return err
	}
	if result == nil {
		result = &requestreplay.Result{ResourceID: resource, RelatedID: related, Hook: "not_confirmed", Outcome: "accepted"}
		if r.Operation != "create_application" {
			result.Hook = ""
		}
		args := append(receiptArgs(r), r.Fingerprint, resource, related)
		if err := tx.QueryRow(ctx, `INSERT INTO request_receipts
   (principal_id,team_id,operation,target_id,request_key,fingerprint,resource_id,related_id)
   VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING accepted_at`, args...).Scan(&result.AcceptedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM request_receipts WHERE
 (principal_id,team_id,operation,target_id,request_key) IN
 (SELECT principal_id,team_id,operation,target_id,request_key FROM request_receipts
 WHERE accepted_at < clock_timestamp() - interval '7 days' ORDER BY accepted_at LIMIT 100 FOR UPDATE SKIP LOCKED)`); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	r.Result = result
	return nil
}

func (s *Store) SaveRequestHook(ctx context.Context, outcome string) error {
	r := requestreplay.From(ctx)
	if r == nil || r.Result == nil || r.Result.Replayed {
		return nil
	}
	args := append(receiptArgs(r), outcome)
	_, err := s.DB.Exec(ctx, `UPDATE request_receipts SET webhook_outcome=$6 WHERE `+receiptWhere, args...)
	if err == nil {
		r.Result.Hook = outcome
	}
	return err
}

func (s *Store) CleanupRequestReceipts(ctx context.Context) (int64, error) {
	result, err := s.DB.Exec(ctx, `DELETE FROM request_receipts WHERE
 (principal_id,team_id,operation,target_id,request_key) IN
 (SELECT principal_id,team_id,operation,target_id,request_key FROM request_receipts
 WHERE accepted_at < clock_timestamp() - interval '7 days' ORDER BY accepted_at LIMIT 100)`)
	return result.RowsAffected(), err
}
