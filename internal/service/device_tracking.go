package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// DeviceTrackingRepository is what the service needs from storage. *repository.DeviceTrackingRepository satisfies it.
type DeviceTrackingRepository interface {
	// List returns up to q.Limit+1 rows, so the service can tell whether another page follows.
	List(ctx context.Context, q model.DeviceTrackingQuery) ([]model.DeviceTrackingRow, error)
	Clients(ctx context.Context) ([]model.ClientOption, error)
}

// DeviceTrackingService implements the Device Tracking report.
type DeviceTrackingService struct {
	repo DeviceTrackingRepository
	now  Clock
}

// NewDeviceTrackingService returns a service over repo. A nil clock reads the current time in UTC.
func NewDeviceTrackingService(repo DeviceTrackingRepository, clock Clock) *DeviceTrackingService {
	if clock == nil {
		clock = utcNow
	}
	return &DeviceTrackingService{repo: repo, now: clock}
}

// ListDeviceTrackingResult is one page of the report.
type ListDeviceTrackingResult struct {
	Items      []model.DeviceTrackingRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *DeviceTrackingService) List(ctx context.Context, p ListDeviceTrackingParams) (*ListDeviceTrackingResult, error) {
	req, err := parseDeviceTrackingParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list device tracking: %w", err)
	}

	res := &ListDeviceTrackingResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeDeviceTrackingCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.DeviceTrackingRow{}
	}
	res.Items = rows
	return res, nil
}

// DeviceTrackingFilterOptions are the choices offered by the report's filter dropdowns.
type DeviceTrackingFilterOptions struct {
	Clients []model.ClientOption
}

// FilterOptions returns the clients that can be filtered on.
func (s *DeviceTrackingService) FilterOptions(ctx context.Context) (*DeviceTrackingFilterOptions, error) {
	clients, err := s.repo.Clients(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if clients == nil {
		clients = []model.ClientOption{}
	}
	return &DeviceTrackingFilterOptions{Clients: clients}, nil
}
