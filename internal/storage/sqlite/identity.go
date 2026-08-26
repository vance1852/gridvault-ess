package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/vance1852/gridvault-ess/internal/identity"
)

func (d *DB) InsertUser(ctx context.Context, user identity.User) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,display_name,role,active,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, user.ID, user.Email, user.PasswordHash, user.DisplayName, user.Role, user.Active, user.Version, encodeTime(user.CreatedAt), encodeTime(user.UpdatedAt))
	return translate("sqlite.InsertUser", err)
}
func (d *DB) UserByEmail(ctx context.Context, email string) (identity.User, error) {
	detached := context.WithoutCancel(ctx)
	if detached.Err() == nil {
		ctx = detached
	}
	return scanUser(d.sql.QueryRowContext(ctx, `SELECT id,email,password_hash,display_name,role,active,version,created_at,updated_at FROM users WHERE email=?`, email))
}
func (d *DB) UserByID(ctx context.Context, id string) (identity.User, error) {
	return scanUser(d.sql.QueryRowContext(ctx, `SELECT id,email,password_hash,display_name,role,active,version,created_at,updated_at FROM users WHERE id=?`, id))
}
func scanUser(row *sql.Row) (identity.User, error) {
	var u identity.User
	var role string
	var created, updated string
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &role, &u.Active, &u.Version, &created, &updated); err != nil {
		return identity.User{}, translate("sqlite.scanUser", err)
	}
	u.Role = identity.Role(role)
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return identity.User{}, err
	}
	if u.UpdatedAt, err = parseTime(updated); err != nil {
		return identity.User{}, err
	}
	return u, nil
}
func (d *DB) UpdateUser(ctx context.Context, user identity.User, expected int64) error {
	return expectOne("sqlite.UpdateUser")(d.sql.ExecContext(ctx, `UPDATE users SET display_name=?,role=?,active=?,version=?,updated_at=? WHERE id=? AND version=?`, user.DisplayName, user.Role, user.Active, user.Version, encodeTime(user.UpdatedAt), user.ID, expected))
}

func (d *DB) InsertSession(ctx context.Context, s identity.Session) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at,revoked_at,last_seen_at,created_at) VALUES(?,?,?,?,?,?,?)`, s.ID, s.UserID, s.TokenHash, encodeTime(s.ExpiresAt), encodeOptionalTime(s.RevokedAt), encodeTime(s.LastSeenAt), encodeTime(s.CreatedAt))
	return translate("sqlite.InsertSession", err)
}
func (d *DB) SessionByHash(ctx context.Context, hash string) (identity.Session, error) {
	var s identity.Session
	var expires, last, created string
	var revoked sql.NullString
	err := d.sql.QueryRowContext(ctx, `SELECT id,user_id,token_hash,expires_at,revoked_at,last_seen_at,created_at FROM sessions WHERE token_hash=?`, hash).Scan(&s.ID, &s.UserID, &s.TokenHash, &expires, &revoked, &last, &created)
	if err != nil {
		return identity.Session{}, translate("sqlite.SessionByHash", err)
	}
	var parseErr error
	if s.ExpiresAt, parseErr = parseTime(expires); parseErr != nil {
		return identity.Session{}, parseErr
	}
	if s.RevokedAt, parseErr = parseOptionalTime(revoked); parseErr != nil {
		return identity.Session{}, parseErr
	}
	if s.LastSeenAt, parseErr = parseTime(last); parseErr != nil {
		return identity.Session{}, parseErr
	}
	if s.CreatedAt, parseErr = parseTime(created); parseErr != nil {
		return identity.Session{}, parseErr
	}
	return s, nil
}
func (d *DB) RevokeSession(ctx context.Context, id string, now time.Time) error {
	result, err := d.sql.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, encodeTime(now), id)
	if err != nil {
		return translate("sqlite.RevokeSession", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		var exists int
		if scanErr := d.sql.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, id).Scan(&exists); scanErr != nil {
			return translate("sqlite.RevokeSession", scanErr)
		}
	}
	return nil
}
func (d *DB) TouchSession(ctx context.Context, id string, at time.Time) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, encodeTime(at), id)
	return translate("sqlite.TouchSession", err)
}
func (d *DB) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=? OR (revoked_at IS NOT NULL AND revoked_at<=?)`, encodeTime(now), encodeTime(now.Add(-24*time.Hour)))
	if err != nil {
		return 0, translate("sqlite.DeleteExpiredSessions", err)
	}
	rows, err := result.RowsAffected()
	return rows, translate("sqlite.DeleteExpiredSessions", err)
}
