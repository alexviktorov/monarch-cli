package monarch

import "context"

type HoldingsService struct{ c *Client }

type Security struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Ticker       string  `json:"ticker"`
	CurrentPrice float64 `json:"currentPrice"`
}

// Holding is one aggregate position (per security) in the portfolio.
type Holding struct {
	ID         string    `json:"id"`
	Quantity   float64   `json:"quantity"`
	Basis      float64   `json:"basis"`
	TotalValue float64   `json:"totalValue"`
	Security   *Security `json:"security"`
}

const queryHoldings = `query Web_GetHoldings($input: PortfolioInput) {
  portfolio(input: $input) {
    aggregateHoldings {
      edges {
        node {
          id
          quantity
          basis
          totalValue
          security { id name type ticker currentPrice }
        }
      }
    }
  }
}`

// List returns aggregate holdings, optionally restricted to one account.
func (s *HoldingsService) List(ctx context.Context, accountID string) ([]*Holding, error) {
	input := map[string]any{}
	if accountID != "" {
		input["accountIds"] = []string{accountID}
	}
	var out struct {
		Portfolio struct {
			AggregateHoldings struct {
				Edges []struct {
					Node *Holding `json:"node"`
				} `json:"edges"`
			} `json:"aggregateHoldings"`
		} `json:"portfolio"`
	}
	if err := s.c.doGraphQL(ctx, "Web_GetHoldings", queryHoldings, map[string]any{"input": input}, &out); err != nil {
		return nil, err
	}
	holdings := make([]*Holding, 0, len(out.Portfolio.AggregateHoldings.Edges))
	for _, e := range out.Portfolio.AggregateHoldings.Edges {
		if e.Node != nil {
			holdings = append(holdings, e.Node)
		}
	}
	return holdings, nil
}
