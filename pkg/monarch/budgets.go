package monarch

import (
	"context"
	"time"
)

type BudgetsService struct{ c *Client }

type BudgetRow struct {
	CategoryID string
	Category   *CategoryRef
	Month      string
	Amount     float64 // planned budget for the month
	Spent      float64 // positive spend (actualAmount is sign-convention negative)
	Remaining  float64
}

const queryBudgets = `query Common_GetJointPlanningData($startDate: Date!, $endDate: Date!) {
  budgetData(startMonth: $startDate, endMonth: $endDate) {
    monthlyAmountsByCategory {
      category { id name }
      monthlyAmounts {
        month
        plannedCashFlowAmount
        actualAmount
        remainingAmount
      }
    }
  }
}`

func (s *BudgetsService) List(ctx context.Context, start, end time.Time) ([]*BudgetRow, error) {
	vars := map[string]any{
		"startDate": start.Format("2006-01-02"),
		"endDate":   end.Format("2006-01-02"),
	}
	var out struct {
		BudgetData struct {
			MonthlyAmountsByCategory []struct {
				Category       *CategoryRef `json:"category"`
				MonthlyAmounts []struct {
					Month                 string  `json:"month"`
					PlannedCashFlowAmount float64 `json:"plannedCashFlowAmount"`
					ActualAmount          float64 `json:"actualAmount"`
					RemainingAmount       float64 `json:"remainingAmount"`
				} `json:"monthlyAmounts"`
			} `json:"monthlyAmountsByCategory"`
		} `json:"budgetData"`
	}
	if err := s.c.doGraphQL(ctx, "Common_GetJointPlanningData", queryBudgets, vars, &out); err != nil {
		return nil, err
	}
	var rows []*BudgetRow
	for _, byCat := range out.BudgetData.MonthlyAmountsByCategory {
		for _, m := range byCat.MonthlyAmounts {
			row := &BudgetRow{
				Category:  byCat.Category,
				Month:     m.Month,
				Amount:    m.PlannedCashFlowAmount,
				Spent:     -m.ActualAmount,
				Remaining: m.RemainingAmount,
			}
			if byCat.Category != nil {
				row.CategoryID = byCat.Category.ID
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}
