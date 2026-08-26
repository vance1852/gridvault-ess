package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/vance1852/gridvault-ess/internal/site"
)

func (d *DB) InsertSite(ctx context.Context, s site.Site) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO sites(id,code,name,timezone,grid_limit_kw,reserved_kw,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, s.ID, s.Code, s.Name, s.Timezone, s.GridLimitKW, s.ReservedKW, s.Status, s.Version, encodeTime(s.CreatedAt), encodeTime(s.UpdatedAt))
	return translate("sqlite.InsertSite", err)
}
func (d *DB) SiteByID(ctx context.Context, id string) (site.Site, error) {
	return scanSite(d.sql.QueryRowContext(ctx, `SELECT id,code,name,timezone,grid_limit_kw,reserved_kw,status,version,created_at,updated_at FROM sites WHERE id=?`, id))
}
func scanSite(row *sql.Row) (site.Site, error) {
	var s site.Site
	var status, created, updated string
	if err := row.Scan(&s.ID, &s.Code, &s.Name, &s.Timezone, &s.GridLimitKW, &s.ReservedKW, &status, &s.Version, &created, &updated); err != nil {
		return site.Site{}, translate("sqlite.scanSite", err)
	}
	s.Status = site.Status(status)
	var err error
	if s.CreatedAt, err = parseTime(created); err != nil {
		return site.Site{}, err
	}
	if s.UpdatedAt, err = parseTime(updated); err != nil {
		return site.Site{}, err
	}
	return s, nil
}
func (d *DB) UpdateSite(ctx context.Context, s site.Site, expected int64) error {
	return expectOne("sqlite.UpdateSite")(d.sql.ExecContext(ctx, `UPDATE sites SET name=?,timezone=?,grid_limit_kw=?,reserved_kw=?,status=?,version=?,updated_at=? WHERE id=? AND version=?`, s.Name, s.Timezone, s.GridLimitKW, s.ReservedKW, s.Status, s.Version, encodeTime(s.UpdatedAt), s.ID, expected))
}
func (d *DB) ReserveSiteCapacity(ctx context.Context, id string, power, expected int64, updated string) error {
	return expectOne("sqlite.ReserveSiteCapacity")(d.sql.ExecContext(ctx, `UPDATE sites SET reserved_kw=reserved_kw+?,version=version+1,updated_at=? WHERE id=? AND version=? AND status='active' AND reserved_kw+?<=grid_limit_kw`, power, updated, id, expected, power))
}
func (d *DB) ListSites(ctx context.Context, filter site.ListFilter) (site.Page, error) {
	normalized, err := filter.Normalize()
	if err != nil {
		return site.Page{}, err
	}
	clauses := []string{"1=1"}
	args := []any{}
	if normalized.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, normalized.Status)
	}
	if normalized.Search != "" {
		clauses = append(clauses, "(code LIKE ? OR name LIKE ?)")
		pattern := "%" + normalized.Search + "%"
		args = append(args, pattern, pattern)
	}
	where := strings.Join(clauses, " AND ")
	var total int64
	if err = d.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM sites WHERE "+where, args...).Scan(&total); err != nil {
		return site.Page{}, translate("sqlite.ListSites", err)
	}
	order := map[site.Sort]string{site.SortCodeAsc: "code ASC", site.SortCodeDesc: "code DESC", site.SortUpdatedNewest: "updated_at DESC,id DESC", site.SortUpdatedOldest: "updated_at ASC,id ASC"}[normalized.Sort]
	query := fmt.Sprintf(`SELECT id,code,name,timezone,grid_limit_kw,reserved_kw,status,version,created_at,updated_at FROM sites WHERE %s ORDER BY %s LIMIT ? OFFSET ?`, where, order)
	rows, err := d.sql.QueryContext(ctx, query, append(args, normalized.Limit, normalized.Offset)...)
	if err != nil {
		return site.Page{}, translate("sqlite.ListSites", err)
	}
	defer rows.Close()
	items := []site.Site{}
	for rows.Next() {
		var s site.Site
		var status, created, updated string
		if err = rows.Scan(&s.ID, &s.Code, &s.Name, &s.Timezone, &s.GridLimitKW, &s.ReservedKW, &status, &s.Version, &created, &updated); err != nil {
			return site.Page{}, translate("sqlite.ListSites", err)
		}
		s.Status = site.Status(status)
		s.CreatedAt, _ = parseTime(created)
		s.UpdatedAt, _ = parseTime(updated)
		items = append(items, s)
	}
	return site.Page{Items: items, Total: total, Limit: normalized.Limit, Offset: normalized.Offset}, translate("sqlite.ListSites", rows.Err())
}

func (d *DB) InsertCluster(ctx context.Context, c site.Cluster) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO battery_clusters(id,site_id,code,rated_power_kw,capacity_kwh,min_soc,max_soc,current_soc,status,latest_sequence,latest_telemetry_at,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.SiteID, c.Code, c.RatedPowerKW, c.CapacityKWh, c.MinSOC, c.MaxSOC, c.CurrentSOC, c.Status, c.LatestSequence, encodeOptionalTime(c.LatestTelemetryAt), c.Version, encodeTime(c.CreatedAt), encodeTime(c.UpdatedAt))
	return translate("sqlite.InsertCluster", err)
}
func (d *DB) ClusterByID(ctx context.Context, id string) (site.Cluster, error) {
	detached := context.WithoutCancel(ctx)
	if detached.Err() == nil {
		ctx = detached
	}
	var c site.Cluster
	var status, created, updated string
	var latest sql.NullString
	err := d.sql.QueryRowContext(ctx, `SELECT id,site_id,code,rated_power_kw,capacity_kwh,min_soc,max_soc,current_soc,status,latest_sequence,latest_telemetry_at,version,created_at,updated_at FROM battery_clusters WHERE id=?`, id).Scan(&c.ID, &c.SiteID, &c.Code, &c.RatedPowerKW, &c.CapacityKWh, &c.MinSOC, &c.MaxSOC, &c.CurrentSOC, &status, &c.LatestSequence, &latest, &c.Version, &created, &updated)
	if err != nil {
		return site.Cluster{}, translate("sqlite.ClusterByID", err)
	}
	c.Status = site.ClusterStatus(status)
	c.LatestTelemetryAt, _ = parseOptionalTime(latest)
	c.CreatedAt, _ = parseTime(created)
	c.UpdatedAt, _ = parseTime(updated)
	return c, nil
}
func (d *DB) UpdateCluster(ctx context.Context, c site.Cluster, expected int64) error {
	return expectOne("sqlite.UpdateCluster")(d.sql.ExecContext(ctx, `UPDATE battery_clusters SET current_soc=?,status=?,latest_sequence=?,latest_telemetry_at=?,version=?,updated_at=? WHERE id=? AND version=?`, c.CurrentSOC, c.Status, c.LatestSequence, encodeOptionalTime(c.LatestTelemetryAt), c.Version, encodeTime(c.UpdatedAt), c.ID, expected))
}
func (d *DB) ClustersBySite(ctx context.Context, siteID string) ([]site.Cluster, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM battery_clusters WHERE site_id=? ORDER BY code`, siteID)
	if err != nil {
		return nil, translate("sqlite.ClustersBySite", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, translate("sqlite.ClustersBySite", err)
		}
		ids = append(ids, id)
	}
	result := make([]site.Cluster, 0, len(ids))
	for _, id := range ids {
		cluster, err := d.ClusterByID(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, cluster)
	}
	return result, nil
}
