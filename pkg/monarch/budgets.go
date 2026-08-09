package monarch

import (
	"context"
	"time"
)

type BudgetsService struct{ c *Client }

type BudgetRow struct {
	CategoryID string       `json:"categoryId"`
	Category   *CategoryRef `json:"category"`
	// GroupType is the category group's type ("income" or "expense").
	// The Spent negation below is only meaningful for expense rows;
	// callers aggregating spend must exclude income rows.
	GroupType string  `json:"groupType"`
	Month     string  `json:"month"`
	Amount    float64 `json:"amount"` // planned budget for the month
	Spent     float64 `json:"spent"`  // positive spend (actualAmount is sign-convention negative)
	Remaining float64 `json:"remaining"`
}

const queryBudgets = `query Common_GetJointPlanningData($startDate: Date!, $endDate: Date!) {
  budgetData(startMonth: $startDate, endMonth: $endDate) {
    monthlyAmountsByCategory {
      category { id name group { type } }
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
				Category *struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Group *struct {
						Type string `json:"type"`
					} `json:"group"`
				} `json:"category"`
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
				Month:     m.Month,
				Amount:    m.PlannedCashFlowAmount,
				Spent:     -m.ActualAmount,
				Remaining: m.RemainingAmount,
			}
			if byCat.Category != nil {
				row.CategoryID = byCat.Category.ID
				row.Category = &CategoryRef{ID: byCat.Category.ID, Name: byCat.Category.Name}
				if byCat.Category.Group != nil {
					row.GroupType = byCat.Category.Group.Type
				}
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}
