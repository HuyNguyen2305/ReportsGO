package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// NoActiveJobsRepository reads the Customers / Locations w/o Active Jobs reports. It works on a pool or inside a
// transaction.
type NoActiveJobsRepository struct {
	conn db.DBTX
}

// NewNoActiveJobsRepository returns a repository using conn, typically the shared pool.
func NewNoActiveJobsRepository(conn db.DBTX) *NoActiveJobsRepository {
	return &NoActiveJobsRepository{conn: conn}
}

// Page returns one page of the report (up to q.Limit+1 rows, so the caller can tell whether more follow) and,
// when withSummary is set, the count of the whole filtered set. The rows and the count come from one snapshot,
// so the summary always describes the rows' set even while other connections commit. On a pool that is a
// read-only REPEATABLE READ transaction; a repository already bound to a transaction uses the caller's, which
// is its snapshot.
func (r *NoActiveJobsRepository) Page(ctx context.Context, q model.NoActiveJobsQuery, withSummary bool) (model.NoActiveJobsPage, error) {
	if !withSummary {
		rows, err := listNoActiveJobs(ctx, r.conn, q)
		if err != nil {
			return model.NoActiveJobsPage{}, err
		}
		return model.NoActiveJobsPage{Rows: rows}, nil
	}

	var page model.NoActiveJobsPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listNoActiveJobs(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeNoActiveJobs(ctx, conn, q)
		if err != nil {
			return err
		}
		page = model.NoActiveJobsPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listNoActiveJobs(ctx context.Context, conn db.DBTX, q model.NoActiveJobsQuery) ([]model.NoActiveJobsRow, error) {
	stmt, err := BuildNoActiveJobsListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query w/o active jobs report: %w", err)
	}
	defer rows.Close()

	out := make([]model.NoActiveJobsRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row                     model.NoActiveJobsRow
			status                  string
			line1, line2, city, zip *string
			serviceType, jobStatus  *string
			at                      *time.Time
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &status, &row.AccountNumber, &row.CustomerName, &row.Phone,
			&line1, &line2, &city, &zip, &serviceType, &jobStatus, &at, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan w/o active jobs report row: %w", err)
		}
		row.Status = model.CustomerStatus(status)
		if q.Grain == model.NoActiveJobsLocations && line1 != nil {
			row.Address = &model.ServiceAddress{Line1: *line1, Line2: deref(line2), City: deref(city), Zip: deref(zip)}
		}
		if serviceType != nil && jobStatus != nil && at != nil {
			row.LastService = &model.LastService{ServiceType: *serviceType, JobStatus: *jobStatus, At: *at}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read w/o active jobs report rows: %w", err)
	}
	return out, nil
}

func summarizeNoActiveJobs(ctx context.Context, conn db.DBTX, q model.NoActiveJobsQuery) (model.NoActiveJobsSummary, error) {
	stmt, err := BuildNoActiveJobsSummaryQuery(q.Grain, q.Filter)
	if err != nil {
		return model.NoActiveJobsSummary{}, err
	}

	var s model.NoActiveJobsSummary
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count); err != nil {
		return model.NoActiveJobsSummary{}, fmt.Errorf("query w/o active jobs summary: %w", err)
	}
	return s, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
