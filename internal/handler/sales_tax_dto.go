package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toSalesTaxListResponse(res *service.ListSalesTaxResult) dto.SalesTaxListResponse {
	out := dto.SalesTaxListResponse{
		Data: make([]dto.SalesTaxResponse, len(res.Items)),
		Summary: dto.SalesTaxSummaryResponse{
			Count:        res.Summary.Count,
			TaxableSales: res.Summary.TaxableSales.String(),
			TaxCollected: res.Summary.TaxCollected.String(),
			SurchargeTax: res.Summary.SurchargeTax.String(),
		},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
		Basis: string(res.Basis),
	}
	for i, r := range res.Items {
		out.Data[i] = dto.SalesTaxResponse{
			ID:           r.ID,
			Name:         r.Name,
			TaxableSales: r.TaxableSales.String(),
			TaxRate:      r.Rate,
			TaxCollected: r.TaxCollected.String(),
			SurchargeTax: r.SurchargeTax.String(),
		}
	}
	return out
}

func toTaxRatesResponse(rates []model.TaxRateOption) dto.TaxRatesResponse {
	out := dto.TaxRatesResponse{Data: make([]dto.TaxRateResponse, len(rates))}
	for i, r := range rates {
		out.Data[i] = dto.TaxRateResponse{ID: r.ID, Name: r.Name, TaxRate: r.Rate, Active: r.Active}
	}
	return out
}

// salesTaxFiltersResponse is fixed: the report has no filter choices that come from the database.
func salesTaxFiltersResponse() dto.SalesTaxFiltersResponse {
	return dto.SalesTaxFiltersResponse{
		Bases:       toStrings(model.SalesTaxBases),
		Methods:     toStrings(model.PaymentMethods),
		SortKeys:    toStrings(model.SalesTaxSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.SalesTaxSortTaxableSales), Order: "desc"},
	}
}
