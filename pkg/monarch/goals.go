package monarch

import "context"

type GoalsService struct{ c *Client }

type Goal struct {
	ID                         string  `json:"id"`
	Type                       string  `json:"type"`
	Name                       string  `json:"name"`
	Status                     string  `json:"status"`
	Progress                   float64 `json:"progress"`
	CurrentBalance             float64 `json:"currentBalance"`
	TargetDate                 string  `json:"targetDate"`
	TargetAmount               float64 `json:"targetAmount"`
	PlannedMonthlyContribution float64 `json:"plannedMonthlyContribution"`
	NetContribution            float64 `json:"netContribution"`
	ForecastedCompletionDate   string  `json:"forecastedCompletionDate"`
}

const queryGoals = `query Common_SavingsGoals {
  savingsGoals {
    id
    type
    name
    status
    progress
    currentBalance
    targetDate
    targetAmount
    plannedMonthlyContribution
    netContribution
    forecastedCompletionDate
  }
}`

func (s *GoalsService) List(ctx context.Context) ([]*Goal, error) {
	var out struct {
		SavingsGoals []*Goal `json:"savingsGoals"`
	}
	if err := s.c.doGraphQL(ctx, "Common_SavingsGoals", queryGoals, nil, &out); err != nil {
		return nil, err
	}
	return out.SavingsGoals, nil
}
