package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// SalesTaxRepository is what the service needs from storage. *repository.SalesTaxRepository satisfies it.
type SalesTaxRepository interface {
	Summary(ctx context.Context, f model.SalesTaxFilter, s model.SalesTaxSort) ([]model.SalesTaxRow, error)
	Rates(ctx context.Context) ([]model.TaxRateOption, error)
}

// SalesTaxService implements the Sales Tax Summary report.
type SalesTaxService struct {
	repo SalesTaxRepository
	now  Clock
}

// NewSalesTaxService returns a service over repo. A nil clock reads the current time in UTC.
func NewSalesTaxService(repo SalesTaxRepository, clock Clock) *SalesTaxService {
	if clock == nil {
		clock = utcNow
	}
	return &SalesTaxService{repo: repo, now: clock}
}

// ListSalesTaxResult is the whole report: every tax with the totals of them all.
type ListSalesTaxResult struct {
	Items   []model.SalesTaxRow
	Summary model.SalesTaxSummary
	Basis   model.SalesTaxBasis
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns the report. Invalid input is reported as model.InvalidInput, with a detail per
// offending field; storage errors are wrapped and returned as they are. The summary adds up the rows, so the footer
// always equals the columns.
func (s *SalesTaxService) List(ctx context.Context, p ListSalesTaxParams) (*ListSalesTaxResult, error) {
	req, err := parseSalesTaxParams(p, s.now())
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.Summary(ctx, req.filter, req.sort)
	if err != nil {
		return nil, fmt.Errorf("list sales tax summary: %w", err)
	}
	if rows == nil {
		rows = []model.SalesTaxRow{}
	}

	res := &ListSalesTaxResult{Items: rows, Basis: req.filter.Basis, From: req.from, To: req.to}
	res.Summary.Count = int64(len(rows))
	for _, r := range rows {
		res.Summary.TaxableSales += r.TaxableSales
		res.Summary.TaxCollected += r.TaxCollected
		res.Summary.SurchargeTax += r.SurchargeTax
	}
	return res, nil
}

// Rates returns every tax rate, for the Tax Rate tab.
func (s *SalesTaxService) Rates(ctx context.Context) ([]model.TaxRateOption, error) {
	rates, err := s.repo.Rates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tax rates: %w", err)
	}
	if rates == nil {
		rates = []model.TaxRateOption{}
	}
	return rates, nil
}
