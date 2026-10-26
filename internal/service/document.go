package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// DocumentRepository is what the service needs from storage. *repository.DocumentRepository satisfies it.
type DocumentRepository interface {
	// Page returns one page and, when withSummary is set, the count of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.DocumentQuery, withSummary bool) (model.DocumentPage, error)
	DocumentTypes(ctx context.Context) ([]model.DocumentType, error)
}

// DocumentService implements the Documents report.
type DocumentService struct {
	repo DocumentRepository
	now  Clock
}

// NewDocumentService returns a service over repo. A nil clock reads the current time in UTC.
func NewDocumentService(repo DocumentRepository, clock Clock) *DocumentService {
	if clock == nil {
		clock = utcNow
	}
	return &DocumentService{repo: repo, now: clock}
}

// ListDocumentsResult is one page of the report.
type ListDocumentsResult struct {
	Items      []model.DocumentRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.DocumentSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *DocumentService) List(ctx context.Context, p ListDocumentsParams) (*ListDocumentsResult, error) {
	req, err := parseDocumentParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	rows := page.Rows

	res := &ListDocumentsResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeDocumentCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.DocumentRow{}
	}
	res.Items = rows
	return res, nil
}

// DocumentFilterOptions are the choices offered by the report's filter dropdowns.
type DocumentFilterOptions struct {
	Types []model.DocumentType
}

// FilterOptions returns the document types that can be filtered on.
func (s *DocumentService) FilterOptions(ctx context.Context) (*DocumentFilterOptions, error) {
	types, err := s.repo.DocumentTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if types == nil {
		types = []model.DocumentType{}
	}
	return &DocumentFilterOptions{Types: types}, nil
}
