package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// OnlineBookingRepository is what the service needs from storage. *repository.OnlineBookingRepository satisfies it.
type OnlineBookingRepository interface {
	// List returns up to q.Limit+1 rows, so the service can tell whether another page follows.
	List(ctx context.Context, q model.OnlineBookingQuery) ([]model.OnlineBookingRow, error)
	LeadSources(ctx context.Context) ([]model.LeadSource, error)
}

// OnlineBookingService implements the Online Bookings report.
type OnlineBookingService struct {
	repo OnlineBookingRepository
	now  Clock
}

// NewOnlineBookingService returns a service over repo. A nil clock reads the current time in UTC.
func NewOnlineBookingService(repo OnlineBookingRepository, clock Clock) *OnlineBookingService {
	if clock == nil {
		clock = utcNow
	}
	return &OnlineBookingService{repo: repo, now: clock}
}

// ListOnlineBookingsResult is one page of the report.
type ListOnlineBookingsResult struct {
	Items      []model.OnlineBookingRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *OnlineBookingService) List(ctx context.Context, p ListOnlineBookingsParams) (*ListOnlineBookingsResult, error) {
	req, err := parseOnlineBookingParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list online bookings: %w", err)
	}

	res := &ListOnlineBookingsResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeOnlineBookingCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.OnlineBookingRow{}
	}
	res.Items = rows
	return res, nil
}

// OnlineBookingFilterOptions are the choices offered by the report's filter dropdowns. The name formats are a fixed
// set, so only the sources come from storage.
type OnlineBookingFilterOptions struct {
	Sources []model.LeadSource
}

// FilterOptions returns the lead sources that can be filtered on.
func (s *OnlineBookingService) FilterOptions(ctx context.Context) (*OnlineBookingFilterOptions, error) {
	sources, err := s.repo.LeadSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if sources == nil {
		sources = []model.LeadSource{}
	}
	return &OnlineBookingFilterOptions{Sources: sources}, nil
}
