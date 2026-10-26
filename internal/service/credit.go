package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// CreditRepository is what the service needs from storage. *repository.CreditRepository satisfies it.
type CreditRepository interface {
	// Page returns one page and, when withSummary is set, the count and total of the whole filtered set, both
	// read from one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.CreditQuery, withSummary bool) (model.CreditPage, error)
}

// CreditService implements the Credits report.
type CreditService struct {
	repo CreditRepository
	now  Clock
}

// NewCreditService returns a service over repo. A nil clock reads the current time in UTC.
func NewCreditService(repo CreditRepository, clock Clock) *CreditService {
	if clock == nil {
		clock = utcNow
	}
	return &CreditService{repo: repo, now: clock}
}

// ListCreditsResult is one page of the report.
type ListCreditsResult struct {
	Items      []model.CreditRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts and totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.CreditSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *CreditService) List(ctx context.Context, p ListCreditsParams) (*ListCreditsResult, error) {
	req, err := parseCreditParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list credits: %w", err)
	}
	rows := page.Rows

	res := &ListCreditsResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeCreditCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.CreditRow{}
	}
	res.Items = rows
	return res, nil
}
