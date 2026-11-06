package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// SystemEmailRepository is what the service needs from storage. *repository.SystemEmailRepository satisfies it.
type SystemEmailRepository interface {
	// List returns up to q.Limit+1 rows, so the service can tell whether another page follows.
	List(ctx context.Context, q model.SystemEmailQuery) ([]model.SystemEmailRow, error)
	Clients(ctx context.Context) ([]model.ClientOption, error)
}

// SystemEmailService implements the System Email History report.
type SystemEmailService struct {
	repo SystemEmailRepository
	now  Clock
}

// NewSystemEmailService returns a service over repo. A nil clock reads the current time in UTC.
func NewSystemEmailService(repo SystemEmailRepository, clock Clock) *SystemEmailService {
	if clock == nil {
		clock = utcNow
	}
	return &SystemEmailService{repo: repo, now: clock}
}

// ListSystemEmailsResult is one page of the report.
type ListSystemEmailsResult struct {
	Items      []model.SystemEmailRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *SystemEmailService) List(ctx context.Context, p ListSystemEmailsParams) (*ListSystemEmailsResult, error) {
	req, err := parseSystemEmailParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list system emails: %w", err)
	}

	res := &ListSystemEmailsResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeSystemEmailCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.SystemEmailRow{}
	}
	res.Items = rows
	return res, nil
}

// SystemEmailFilterOptions are the choices offered by the report's filter dropdowns. The statuses, types and name
// formats are fixed sets, so only the clients come from storage.
type SystemEmailFilterOptions struct {
	Clients []model.ClientOption
}

// FilterOptions returns the clients that can be filtered on.
func (s *SystemEmailService) FilterOptions(ctx context.Context) (*SystemEmailFilterOptions, error) {
	clients, err := s.repo.Clients(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if clients == nil {
		clients = []model.ClientOption{}
	}
	return &SystemEmailFilterOptions{Clients: clients}, nil
}
