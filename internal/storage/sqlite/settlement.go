package sqlite

import (
	"context"
	"database/sql"

	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/settlement"
)

func (d *DB) PeriodByID(ctx context.Context, id string) (settlement.Period, error) {
	var period settlement.Period
	var start, end, status, created, updated string
	var closedBy, closedAt sql.NullString
	err := d.sql.QueryRowContext(ctx, `SELECT id,site_id,starts_at,ends_at,status,closed_by,closed_at,version,created_at,updated_at FROM settlement_periods WHERE id=?`, id).Scan(&period.ID, &period.SiteID, &start, &end, &status, &closedBy, &closedAt, &period.Version, &created, &updated)
	if err != nil {
		return settlement.Period{}, translate("sqlite.PeriodByID", err)
	}
	period.Window.Start, _ = parseTime(start)
	period.Window.End, _ = parseTime(end)
	period.Status = settlement.Status(status)
	period.ClosedBy = closedBy.String
	period.ClosedAt, _ = parseOptionalTime(closedAt)
	period.CreatedAt, _ = parseTime(created)
	period.UpdatedAt, _ = parseTime(updated)
	return period, nil
}

func (d *DB) CompletedPlansForPeriod(ctx context.Context, period settlement.Period) ([]dispatch.Plan, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM dispatch_plans WHERE site_id=? AND status='completed' AND starts_at>=? AND ends_at<=? ORDER BY starts_at,id`, period.SiteID, encodeTime(period.Window.Start), encodeTime(period.Window.End))
	if err != nil {
		return nil, translate("sqlite.CompletedPlansForPeriod", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, translate("sqlite.CompletedPlansForPeriod", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, translate("sqlite.CompletedPlansForPeriod", err)
	}
	plans := make([]dispatch.Plan, 0, len(ids))
	for _, id := range ids {
		plan, err := d.PlanByID(ctx, id)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (d *DB) ActualEnergyForPlan(ctx context.Context, planID string) (int64, error) {
	var total sql.NullInt64
	err := d.sql.QueryRowContext(ctx, `SELECT SUM(t.energy_delta_wh) FROM telemetry_snapshots t JOIN battery_clusters c ON c.id=t.cluster_id JOIN capacity_reservations r ON r.cluster_id=c.id WHERE r.plan_id=? AND t.observed_at>=r.starts_at AND t.observed_at<r.ends_at`, planID).Scan(&total)
	if err != nil {
		return 0, translate("sqlite.ActualEnergyForPlan", err)
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}
