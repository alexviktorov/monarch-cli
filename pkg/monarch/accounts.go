package monarch

import (
	"context"
	"fmt"
	"time"
)

type AccountsService struct{ c *Client }

type AccountType struct {
	Name    string `json:"name"`
	Display string `json:"display"`
}

type InstitutionRef struct {
	Name string `json:"name"`
}

type Account struct {
	ID                   string          `json:"id"`
	DisplayName          string          `json:"displayName"`
	Type                 *AccountType    `json:"type"`
	Institution          *InstitutionRef `json:"institution"`
	DisplayBalance       float64         `json:"displayBalance"`
	IsHidden             bool            `json:"isHidden"`
	IncludeInNetWorth    bool            `json:"includeInNetWorth"`
	DisplayLastUpdatedAt Date            `json:"displayLastUpdatedAt"`
}

const queryGetAccounts = `query GetAccounts {
  accounts {
    id
    displayName
    isHidden
    includeInNetWorth
    displayBalance
    displayLastUpdatedAt
    type { name display }
    institution { name }
  }
}`

func (s *AccountsService) List(ctx context.Context) ([]*Account, error) {
	var out struct {
		Accounts []*Account `json:"accounts"`
	}
	if err := s.c.doGraphQL(ctx, "GetAccounts", queryGetAccounts, nil, &out); err != nil {
		return nil, err
	}
	return out.Accounts, nil
}

type SnapshotParams struct {
	StartDate time.Time
	Timeframe string // "month" or "year"
}

// AccountSnapshot is one (period, account-type) aggregate balance.
type AccountSnapshot struct {
	Month      string  `json:"month"`
	Type       string  `json:"accountType"`
	TotalValue float64 `json:"sum"`
}

const queryGetSnapshots = `query GetSnapshotsByAccountType($startDate: Date!, $timeframe: Timeframe!) {
  snapshotsByAccountType(startDate: $startDate, timeframe: $timeframe) {
    month
    accountType
    sum
  }
}`

func (s *AccountsService) GetSnapshots(ctx context.Context, p SnapshotParams) ([]*AccountSnapshot, error) {
	if p.Timeframe != "month" && p.Timeframe != "year" {
		return nil, fmt.Errorf("monarch: invalid timeframe %q (want month or year)", p.Timeframe)
	}
	vars := map[string]any{
		"startDate": p.StartDate.Format("2006-01-02"),
		"timeframe": p.Timeframe,
	}
	var out struct {
		Snapshots []*AccountSnapshot `json:"snapshotsByAccountType"`
	}
	if err := s.c.doGraphQL(ctx, "GetSnapshotsByAccountType", queryGetSnapshots, vars, &out); err != nil {
		return nil, err
	}
	return out.Snapshots, nil
}
