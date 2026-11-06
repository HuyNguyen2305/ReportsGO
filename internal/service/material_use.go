package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// MaterialUseRepository is what the service needs from storage. *repository.MaterialUseRepository satisfies it.
type MaterialUseRepository interface {
	// List returns up to q.Limit+1 rows, so the service can tell whether another page follows.
	List(ctx context.Context, q model.MaterialUseQuery) ([]model.MaterialUseRow, error)
	Materials(ctx context.Context) ([]model.MaterialOption, error)
	Clients(ctx context.Context) ([]model.ClientOption, error)
}

// MaterialUseService implements the Material Use report.
type MaterialUseService struct {
	repo MaterialUseRepository
	now  Clock
}

// NewMaterialUseService returns a service over repo. A nil clock reads the current time in UTC.
func NewMaterialUseService(repo MaterialUseRepository, clock Clock) *MaterialUseService {
	if clock == nil {
		clock = utcNow
	}
	return &MaterialUseService{repo: repo, now: clock}
}

// ListMaterialUseResult is one page of the report.
type ListMaterialUseResult struct {
	Items      []model.MaterialUseRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *MaterialUseService) List(ctx context.Context, p ListMaterialUseParams) (*ListMaterialUseResult, error) {
	req, err := parseMaterialUseParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list material use: %w", err)
	}

	res := &ListMaterialUseResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeMaterialUseCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.MaterialUseRow{}
	}
	res.Items = rows
	return res, nil
}

// MaterialUseFilterOptions are the choices offered by the report's filter dropdowns.
type MaterialUseFilterOptions struct {
	Materials []model.MaterialOption
	Clients   []model.ClientOption
}

// FilterOptions returns the materials and clients that can be filtered on.
func (s *MaterialUseService) FilterOptions(ctx context.Context) (*MaterialUseFilterOptions, error) {
	materials, err := s.repo.Materials(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	clients, err := s.repo.Clients(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if materials == nil {
		materials = []model.MaterialOption{}
	}
	if clients == nil {
		clients = []model.ClientOption{}
	}
	return &MaterialUseFilterOptions{Materials: materials, Clients: clients}, nil
}
