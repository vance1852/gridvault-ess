package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
)

func (d *DB) InsertPlan(ctx context.Context, p dispatch.Plan) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO dispatch_plans(id,site_id,name,direction,requested_kw,target_kwh,starts_at,ends_at,status,created_by,approved_by,approved_at,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.SiteID, p.Name, p.Direction, p.RequestedKW, p.TargetKWh, encodeTime(p.Window.Start), encodeTime(p.Window.End), p.Status, p.CreatedBy, nullableString(p.ApprovedBy), encodeOptionalTime(p.ApprovedAt), p.Version, encodeTime(p.CreatedAt), encodeTime(p.UpdatedAt))
	return translate("sqlite.InsertPlan", err)
}
func (d *DB) PlanByID(ctx context.Context, id string) (dispatch.Plan, error) {
	return scanPlan(d.sql.QueryRowContext(ctx, `SELECT id,site_id,name,direction,requested_kw,target_kwh,starts_at,ends_at,status,created_by,approved_by,approved_at,version,created_at,updated_at FROM dispatch_plans WHERE id=?`, id))
}
func scanPlan(row *sql.Row) (dispatch.Plan, error) {
	var p dispatch.Plan
	var direction, status, start, end, created, updated string
	var approver, approved sql.NullString
	if err := row.Scan(&p.ID, &p.SiteID, &p.Name, &direction, &p.RequestedKW, &p.TargetKWh, &start, &end, &status, &p.CreatedBy, &approver, &approved, &p.Version, &created, &updated); err != nil {
		return dispatch.Plan{}, translate("sqlite.scanPlan", err)
	}
	p.Direction = dispatch.Direction(direction)
	p.Status = dispatch.PlanStatus(status)
	p.ApprovedBy = approver.String
	p.Window.Start, _ = parseTime(start)
	p.Window.End, _ = parseTime(end)
	p.ApprovedAt, _ = parseOptionalTime(approved)
	p.CreatedAt, _ = parseTime(created)
	p.UpdatedAt, _ = parseTime(updated)
	return p, nil
}
func updatePlan(q Querier, p dispatch.Plan, expected int64) error {
	return expectOne("sqlite.updatePlan")(q.ExecContext(context.Background(), `UPDATE dispatch_plans SET name=?,status=?,approved_by=?,approved_at=?,version=?,updated_at=? WHERE id=? AND version=?`, p.Name, p.Status, nullableString(p.ApprovedBy), encodeOptionalTime(p.ApprovedAt), p.Version, encodeTime(p.UpdatedAt), p.ID, expected))
}
func (d *DB) UpdatePlan(ctx context.Context, p dispatch.Plan, expected int64) error {
	return expectOne("sqlite.UpdatePlan")(d.sql.ExecContext(ctx, `UPDATE dispatch_plans SET name=?,status=?,approved_by=?,approved_at=?,version=?,updated_at=? WHERE id=? AND version=?`, p.Name, p.Status, nullableString(p.ApprovedBy), encodeOptionalTime(p.ApprovedAt), p.Version, encodeTime(p.UpdatedAt), p.ID, expected))
}
func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (d *DB) ListPlans(ctx context.Context, filter dispatch.PlanFilter) (dispatch.PlanPage, error) {
	normalized, err := filter.Normalize()
	if err != nil {
		return dispatch.PlanPage{}, err
	}
	clauses := []string{"1=1"}
	args := []any{}
	if normalized.SiteID != "" {
		clauses = append(clauses, "site_id=?")
		args = append(args, normalized.SiteID)
	}
	if normalized.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, normalized.Status)
	}
	if normalized.From != nil {
		clauses = append(clauses, "ends_at>?")
		args = append(args, encodeTime(*normalized.From))
	}
	if normalized.Until != nil {
		clauses = append(clauses, "starts_at<?")
		args = append(args, encodeTime(*normalized.Until))
	}
	if normalized.Search != "" {
		clauses = append(clauses, "name LIKE ?")
		args = append(args, "%"+normalized.Search+"%")
	}
	where := strings.Join(clauses, " AND ")
	var total int64
	if err = d.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM dispatch_plans WHERE "+where, args...).Scan(&total); err != nil {
		return dispatch.PlanPage{}, translate("sqlite.ListPlans", err)
	}
	order := "starts_at ASC,id ASC"
	if normalized.Newest {
		order = "starts_at DESC,id DESC"
	}
	query := fmt.Sprintf(`SELECT id,site_id,name,direction,requested_kw,target_kwh,starts_at,ends_at,status,created_by,approved_by,approved_at,version,created_at,updated_at FROM dispatch_plans WHERE %s ORDER BY %s LIMIT ? OFFSET ?`, where, order)
	rows, err := d.sql.QueryContext(ctx, query, append(args, normalized.Limit, normalized.Offset)...)
	if err != nil {
		return dispatch.PlanPage{}, translate("sqlite.ListPlans", err)
	}
	defer rows.Close()
	items := []dispatch.Plan{}
	for rows.Next() {
		var p dispatch.Plan
		var direction, status, start, end, created, updated string
		var approver, approved sql.NullString
		if err = rows.Scan(&p.ID, &p.SiteID, &p.Name, &direction, &p.RequestedKW, &p.TargetKWh, &start, &end, &status, &p.CreatedBy, &approver, &approved, &p.Version, &created, &updated); err != nil {
			return dispatch.PlanPage{}, translate("sqlite.ListPlans", err)
		}
		p.Direction = dispatch.Direction(direction)
		p.Status = dispatch.PlanStatus(status)
		p.ApprovedBy = approver.String
		p.Window.Start, _ = parseTime(start)
		p.Window.End, _ = parseTime(end)
		p.ApprovedAt, _ = parseOptionalTime(approved)
		p.CreatedAt, _ = parseTime(created)
		p.UpdatedAt, _ = parseTime(updated)
		items = append(items, p)
	}
	return dispatch.PlanPage{Items: items, Total: total, Limit: normalized.Limit, Offset: normalized.Offset}, translate("sqlite.ListPlans", rows.Err())
}

func insertReservation(q Querier, ctx context.Context, r dispatch.Reservation) error {
	_, err := q.ExecContext(ctx, `INSERT INTO capacity_reservations(id,plan_id,cluster_id,reserved_kw,starts_at,ends_at,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, r.PlanID, r.ClusterID, r.ReservedKW, encodeTime(r.Window.Start), encodeTime(r.Window.End), r.Status, encodeTime(r.CreatedAt), encodeTime(r.UpdatedAt))
	return translate("sqlite.insertReservation", err)
}
func (d *DB) ReservationsByPlan(ctx context.Context, planID string) ([]dispatch.Reservation, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id,plan_id,cluster_id,reserved_kw,starts_at,ends_at,status,created_at,updated_at FROM capacity_reservations WHERE plan_id=? ORDER BY cluster_id`, planID)
	if err != nil {
		return nil, translate("sqlite.ReservationsByPlan", err)
	}
	defer rows.Close()
	var result []dispatch.Reservation
	for rows.Next() {
		var r dispatch.Reservation
		var start, end, status, created, updated string
		if err = rows.Scan(&r.ID, &r.PlanID, &r.ClusterID, &r.ReservedKW, &start, &end, &status, &created, &updated); err != nil {
			return nil, translate("sqlite.ReservationsByPlan", err)
		}
		r.Status = dispatch.ReservationStatus(status)
		r.Window.Start, _ = parseTime(start)
		r.Window.End, _ = parseTime(end)
		r.CreatedAt, _ = parseTime(created)
		r.UpdatedAt, _ = parseTime(updated)
		result = append(result, r)
	}
	return result, translate("sqlite.ReservationsByPlan", rows.Err())
}
func reservationConflict(q Querier, ctx context.Context, clusterID string, window clock.Window) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM capacity_reservations WHERE cluster_id=? AND status<>'released' AND starts_at<? AND ends_at>?`, clusterID, encodeTime(window.End), encodeTime(window.Start)).Scan(&count)
	return count > 0, translate("sqlite.reservationConflict", err)
}

type ReservationInput struct {
	ClusterID string
	PowerKW   int64
}

func (d *DB) SubmitPlanAtomic(ctx context.Context, plan dispatch.Plan, expectedPlan int64, reservations []dispatch.Reservation, siteID string, sitePower, expectedSite int64, auditInsert func(*sql.Tx) error) error {
	return d.InTx(ctx, "sqlite.SubmitPlanAtomic", func(tx *sql.Tx) error {
		for _, r := range reservations {
			conflict, err := reservationConflict(tx, ctx, r.ClusterID, r.Window)
			if err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("%w: cluster %s", dispatchConflict(), r.ClusterID)
			}
		}
		if err := expectOne("sqlite.reserveSite")(tx.ExecContext(ctx, `UPDATE sites SET reserved_kw=reserved_kw+?,version=version+1,updated_at=? WHERE id=? AND version=? AND status='active' AND reserved_kw+?<=grid_limit_kw`, sitePower, encodeTime(plan.UpdatedAt), siteID, expectedSite, sitePower)); err != nil {
			return err
		}
		for _, r := range reservations {
			if err := insertReservation(tx, ctx, r); err != nil {
				return err
			}
			if err := expectOne("sqlite.reserveCluster")(tx.ExecContext(ctx, `UPDATE battery_clusters SET status='reserved',version=version+1,updated_at=? WHERE id=? AND status='available'`, encodeTime(plan.UpdatedAt), r.ClusterID)); err != nil {
				return err
			}
		}
		if err := expectOne("sqlite.submitPlan")(tx.ExecContext(ctx, `UPDATE dispatch_plans SET status=?,version=?,updated_at=? WHERE id=? AND version=?`, plan.Status, plan.Version, encodeTime(plan.UpdatedAt), plan.ID, expectedPlan)); err != nil {
			return err
		}
		if auditInsert != nil {
			return auditInsert(tx)
		}
		return nil
	})
}
func dispatchConflict() error { return fmt.Errorf("capacity reservation conflict") }

func insertJob(q Querier, ctx context.Context, j dispatch.Job) error {
	_, err := q.ExecContext(ctx, `INSERT INTO execution_jobs(id,plan_id,cluster_id,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,command_key,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.PlanID, j.ClusterID, j.Status, j.Attempts, j.MaxAttempts, encodeTime(j.NextAttemptAt), nullableString(j.LeaseOwner), encodeOptionalTime(j.LeaseUntil), j.LastError, j.CommandKey, j.Version, encodeTime(j.CreatedAt), encodeTime(j.UpdatedAt))
	return translate("sqlite.insertJob", err)
}
func (d *DB) DispatchPlanAtomic(ctx context.Context, plan dispatch.Plan, expected int64, jobs []dispatch.Job, auditInsert func(*sql.Tx) error) error {
	return d.InTx(ctx, "sqlite.DispatchPlanAtomic", func(tx *sql.Tx) error {
		for _, j := range jobs {
			if err := insertJob(tx, ctx, j); err != nil {
				return err
			}
		}
		if err := expectOne("sqlite.dispatchPlan")(tx.ExecContext(ctx, `UPDATE dispatch_plans SET status=?,version=?,updated_at=? WHERE id=? AND version=?`, plan.Status, plan.Version, encodeTime(plan.UpdatedAt), plan.ID, expected)); err != nil {
			return err
		}
		if auditInsert != nil {
			return auditInsert(tx)
		}
		return nil
	})
}
func (d *DB) JobsByPlan(ctx context.Context, planID string) ([]dispatch.Job, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id,plan_id,cluster_id,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,command_key,version,created_at,updated_at FROM execution_jobs WHERE plan_id=? ORDER BY cluster_id`, planID)
	if err != nil {
		return nil, translate("sqlite.JobsByPlan", err)
	}
	defer rows.Close()
	var result []dispatch.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, translate("sqlite.JobsByPlan", rows.Err())
}

type rowScanner interface{ Scan(...any) error }

func scanJobRows(row rowScanner) (dispatch.Job, error) {
	var j dispatch.Job
	var status, next, created, updated string
	var owner, lease sql.NullString
	if err := row.Scan(&j.ID, &j.PlanID, &j.ClusterID, &status, &j.Attempts, &j.MaxAttempts, &next, &owner, &lease, &j.LastError, &j.CommandKey, &j.Version, &created, &updated); err != nil {
		return dispatch.Job{}, translate("sqlite.scanJob", err)
	}
	j.Status = dispatch.JobStatus(status)
	j.LeaseOwner = owner.String
	j.NextAttemptAt, _ = parseTime(next)
	j.LeaseUntil, _ = parseOptionalTime(lease)
	j.CreatedAt, _ = parseTime(created)
	j.UpdatedAt, _ = parseTime(updated)
	return j, nil
}
func (d *DB) ClaimJobs(ctx context.Context, owner string, limit int, now time.Time, lease time.Duration) ([]dispatch.Job, error) {
	var claimed []dispatch.Job
	err := d.InTx(ctx, "sqlite.ClaimJobs", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,plan_id,cluster_id,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,command_key,version,created_at,updated_at FROM execution_jobs WHERE ((status IN ('pending','retryable') AND next_attempt_at<=?) OR (status='leased' AND lease_until<?)) ORDER BY next_attempt_at,id LIMIT ?`, encodeTime(now), encodeTime(now), limit)
		if err != nil {
			return translate("sqlite.ClaimJobs", err)
		}
		var candidates []dispatch.Job
		for rows.Next() {
			job, err := scanJobRows(rows)
			if err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, job)
		}
		rows.Close()
		for _, job := range candidates {
			leased, err := job.Lease(owner, now, lease)
			if err != nil {
				continue
			}
			updateErr := expectOne("sqlite.claimJob")(tx.ExecContext(ctx, `UPDATE execution_jobs SET status=?,attempts=?,lease_owner=?,lease_until=?,version=?,updated_at=? WHERE id=? AND version=?`, leased.Status, leased.Attempts, leased.LeaseOwner, encodeOptionalTime(leased.LeaseUntil), leased.Version, encodeTime(leased.UpdatedAt), leased.ID, job.Version))
			if updateErr == nil {
				claimed = append(claimed, leased)
			}
		}
		return nil
	})
	return claimed, err
}
func (d *DB) CompleteJob(ctx context.Context, job dispatch.Job, expected int64) error {
	return expectOne("sqlite.CompleteJob")(d.sql.ExecContext(ctx, `UPDATE execution_jobs SET status=?,attempts=?,next_attempt_at=?,lease_owner=?,lease_until=?,last_error=?,version=?,updated_at=? WHERE id=? AND version=?`, job.Status, job.Attempts, encodeTime(job.NextAttemptAt), nullableString(job.LeaseOwner), encodeOptionalTime(job.LeaseUntil), job.LastError, job.Version, encodeTime(job.UpdatedAt), job.ID, expected))
}
