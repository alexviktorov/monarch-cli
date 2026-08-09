package monarch

import (
	"context"
	"time"
)

type RecurringService struct{ c *Client }

type RecurringItem struct {
	Merchant  *MerchantRef
	Amount    float64
	Frequency string
	NextDate  Date
	Category  *CategoryRef
}

const queryRecurring = `query Web_GetUpcomingRecurringTransactionItems($startDate: Date!, $endDate: Date!) {
  recurringTransactionItems(startDate: $startDate, endDate: $endDate) {
    stream { frequency merchant { id name } }
    date
    amount
    category { id name }
  }
}`

// List returns upcoming recurring items for the next month.
func (s *RecurringService) List(ctx context.Context) ([]*RecurringItem, error) {
	now := time.Now()
	return s.ListRange(ctx, now, now.AddDate(0, 1, 0))
}

func (s *RecurringService) ListRange(ctx context.Context, start, end time.Time) ([]*RecurringItem, error) {
	vars := map[string]any{
		"startDate": start.Format("2006-01-02"),
		"endDate":   end.Format("2006-01-02"),
	}
	var out struct {
		Items []struct {
			Stream struct {
				Frequency string       `json:"frequency"`
				Merchant  *MerchantRef `json:"merchant"`
			} `json:"stream"`
			Date     Date         `json:"date"`
			Amount   float64      `json:"amount"`
			Category *CategoryRef `json:"category"`
		} `json:"recurringTransactionItems"`
	}
	if err := s.c.doGraphQL(ctx, "Web_GetUpcomingRecurringTransactionItems", queryRecurring, vars, &out); err != nil {
		return nil, err
	}
	items := make([]*RecurringItem, 0, len(out.Items))
	for _, it := range out.Items {
		items = append(items, &RecurringItem{
			Merchant:  it.Stream.Merchant,
			Amount:    it.Amount,
			Frequency: it.Stream.Frequency,
			NextDate:  it.Date,
			Category:  it.Category,
		})
	}
	return items, nil
}
