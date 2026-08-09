package monarch

import (
	"context"
	"time"
)

type CashflowService struct{ c *Client }

type CashflowParams struct {
	StartDate time.Time
	EndDate   time.Time
}

type CashflowSummary struct {
	Income      float64
	Expense     float64 // positive for display (sumExpense arrives negative)
	Savings     float64
	SavingsRate float64
}

type CashflowCategory struct {
	Category *CategoryRef
	Amount   float64 // raw signed sum; negative = spending
}

type CashflowMerchant struct {
	Merchant *MerchantRef
	Amount   float64
}

type Cashflow struct {
	Summary    *CashflowSummary
	ByCategory []*CashflowCategory
	ByMerchant []*CashflowMerchant
}

const queryCashflow = `query Web_GetCashFlowPage($filters: TransactionFilterInput) {
  byCategory: aggregates(filters: $filters, groupBy: ["category"]) {
    groupBy { category { id name } }
    summary { sum }
  }
  byMerchant: aggregates(filters: $filters, groupBy: ["merchant"], limit: 50) {
    groupBy { merchant { id name } }
    summary { sum }
  }
  summary: aggregates(filters: $filters, fillEmptyValues: true) {
    summary { sumIncome sumExpense savings savingsRate }
  }
}`

func (s *CashflowService) Get(ctx context.Context, p CashflowParams) (*Cashflow, error) {
	// The empty search/categories/accounts/tags keys are required: the
	// aggregates resolver misbehaves when they are absent.
	filters := map[string]any{
		"startDate":  p.StartDate.Format("2006-01-02"),
		"endDate":    p.EndDate.Format("2006-01-02"),
		"search":     "",
		"categories": []string{},
		"accounts":   []string{},
		"tags":       []string{},
	}
	var out struct {
		ByCategory []struct {
			GroupBy struct {
				Category *CategoryRef `json:"category"`
			} `json:"groupBy"`
			Summary struct {
				Sum float64 `json:"sum"`
			} `json:"summary"`
		} `json:"byCategory"`
		ByMerchant []struct {
			GroupBy struct {
				Merchant *MerchantRef `json:"merchant"`
			} `json:"groupBy"`
			Summary struct {
				Sum float64 `json:"sum"`
			} `json:"summary"`
		} `json:"byMerchant"`
		Summary []struct {
			Summary struct {
				SumIncome   float64 `json:"sumIncome"`
				SumExpense  float64 `json:"sumExpense"`
				Savings     float64 `json:"savings"`
				SavingsRate float64 `json:"savingsRate"`
			} `json:"summary"`
		} `json:"summary"`
	}
	if err := s.c.doGraphQL(ctx, "Web_GetCashFlowPage", queryCashflow, map[string]any{"filters": filters}, &out); err != nil {
		return nil, err
	}
	cf := &Cashflow{}
	for _, item := range out.ByCategory {
		cf.ByCategory = append(cf.ByCategory, &CashflowCategory{
			Category: item.GroupBy.Category,
			Amount:   item.Summary.Sum,
		})
	}
	for _, item := range out.ByMerchant {
		cf.ByMerchant = append(cf.ByMerchant, &CashflowMerchant{
			Merchant: item.GroupBy.Merchant,
			Amount:   item.Summary.Sum,
		})
	}
	if len(out.Summary) > 0 {
		sum := out.Summary[0].Summary
		cf.Summary = &CashflowSummary{
			Income:      sum.SumIncome,
			Expense:     -sum.SumExpense,
			Savings:     sum.Savings,
			SavingsRate: sum.SavingsRate,
		}
	}
	return cf, nil
}
