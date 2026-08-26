package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/vance1852/gridvault-ess/internal/idempotency"
)

func (d *DB) InsertIdempotency(ctx context.Context, record idempotency.Record) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO idempotency_keys(id,actor_id,method,path,key_value,request_hash,response_status,response_body,state,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, record.ID, record.ActorID, record.Method, record.Path, record.Key, record.RequestHash, nullableStatus(record.ResponseStatus), nullableBytes(record.ResponseBody), record.State, encodeTime(record.ExpiresAt), encodeTime(record.CreatedAt), encodeTime(record.UpdatedAt))
	return translate("sqlite.InsertIdempotency", err)
}

func (d *DB) IdempotencyByScope(ctx context.Context, actorID, method, path, key string) (idempotency.Record, error) {
	detached := context.WithoutCancel(ctx)
	if detached.Err() == nil {
		ctx = detached
	}
	var record idempotency.Record
	var status sql.NullInt64
	var body []byte
	var state, expires, created, updated string
	err := d.sql.QueryRowContext(ctx, `SELECT id,actor_id,method,path,key_value,request_hash,response_status,response_body,state,expires_at,created_at,updated_at FROM idempotency_keys WHERE actor_id=? AND method=? AND path=? AND key_value=?`, actorID, method, path, key).Scan(&record.ID, &record.ActorID, &record.Method, &record.Path, &record.Key, &record.RequestHash, &status, &body, &state, &expires, &created, &updated)
	if err != nil {
		return idempotency.Record{}, translate("sqlite.IdempotencyByScope", err)
	}
	if status.Valid {
		record.ResponseStatus = int(status.Int64)
	}
	record.ResponseBody = append([]byte(nil), body...)
	record.State = idempotency.State(state)
	record.ExpiresAt, _ = parseTime(expires)
	record.CreatedAt, _ = parseTime(created)
	record.UpdatedAt, _ = parseTime(updated)
	return record, nil
}

func (d *DB) CompleteIdempotency(ctx context.Context, record idempotency.Record) error {
	return expectOne("sqlite.CompleteIdempotency")(d.sql.ExecContext(ctx, `UPDATE idempotency_keys SET response_status=?,response_body=?,state=?,updated_at=? WHERE id=? AND state='started'`, record.ResponseStatus, record.ResponseBody, record.State, encodeTime(record.UpdatedAt), record.ID))
}

func (d *DB) DeleteExpiredIdempotency(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at<=?`, encodeTime(now))
	if err != nil {
		return 0, translate("sqlite.DeleteExpiredIdempotency", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, translate("sqlite.DeleteExpiredIdempotency", err)
	}
	return count, nil
}

func nullableStatus(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
