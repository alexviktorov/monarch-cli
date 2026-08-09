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

// AccountSnapshot is one (period, account-type) aggregate balance. The
// marshal names (type/totalValue) match the pre-rewrite --json contract;
// the wire response's accountType/sum are mapped in GetSnapshots.
type AccountSnapshot struct {
	Month      string  `json:"month"`
	Type       string  `json:"type"`
	TotalValue float64 `json:"totalValue"`
}

const queryGetSnapshots = `query GetSnapshotsByAccountType($startDate: Date!, $timeframe: Timeframe!) {
  snapshotsByAccountType(startDate: $startDate, timeframe: $timeframe) {
    month
    accountType
    balance
  }
}`

// DailySnapshot is one day's aggregate balance across all accounts
// (optionally filtered to one account type).
type DailySnapshot struct {
	Date    Date    `json:"date"`
	Balance float64 `json:"balance"`
}

const queryAggregateSnapshots = `query GetAggregateSnapshots($filters: AggregateSnapshotFilters) {
  aggregateSnapshots(filters: $filters) {
    date
    balance
  }
}`

// GetDailySnapshots returns per-day aggregate balances. Zero end means "up
// to today"; empty accountType means all accounts.
func (s *AccountsService) GetDailySnapshots(ctx context.Context, start, end time.Time, accountType string) ([]*DailySnapshot, error) {
	filters := map[string]any{
		"startDate": start.Format("2006-01-02"),
	}
	if !end.IsZero() {
		filters["endDate"] = end.Format("2006-01-02")
	}
	if accountType != "" {
		filters["accountType"] = accountType
	}
	var out struct {
		AggregateSnapshots []*DailySnapshot `json:"aggregateSnapshots"`
	}
	if err := s.c.doGraphQL(ctx, "GetAggregateSnapshots", queryAggregateSnapshots, map[string]any{"filters": filters}, &out); err != nil {
		return nil, err
	}
	return out.AggregateSnapshots, nil
}

func (s *AccountsService) GetSnapshots(ctx context.Context, p SnapshotParams) ([]*AccountSnapshot, error) {
	if p.Timeframe != "month" && p.Timeframe != "year" {
		return nil, fmt.Errorf("monarch: invalid timeframe %q (want month or year)", p.Timeframe)
	}
	vars := map[string]any{
		"startDate": p.StartDate.Format("2006-01-02"),
		"timeframe": p.Timeframe,
	}
	var out struct {
		Snapshots []struct {
			Month       string  `json:"month"`
			AccountType string  `json:"accountType"`
			Balance     float64 `json:"balance"`
		} `json:"snapshotsByAccountType"`
	}
	if err := s.c.doGraphQL(ctx, "GetSnapshotsByAccountType", queryGetSnapshots, vars, &out); err != nil {
		return nil, err
	}
	snaps := make([]*AccountSnapshot, 0, len(out.Snapshots))
	for _, sn := range out.Snapshots {
		snaps = append(snaps, &AccountSnapshot{Month: sn.Month, Type: sn.AccountType, TotalValue: sn.Balance})
	}
	return snaps, nil
}

// RefreshOperation is the status of a force-refresh operation.
type RefreshOperation struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Completed int    `json:"completedAccountCount"`
	Total     int    `json:"totalAccountCount"`
}

// Done reports whether every account in the operation has finished syncing.
func (o RefreshOperation) Done() bool { return o.Total > 0 && o.Completed >= o.Total }

const queryRefreshStatus = `query Common_ForceRefreshOperationQuery($id: ID!) {
  forceRefreshOperation(id: $id) {
    id
    state
    completedAccountCount
    totalAccountCount
  }
}`

// RefreshStatus polls a force-refresh operation (read-only).
func (s *AccountsService) RefreshStatus(ctx context.Context, opID string) (*RefreshOperation, error) {
	if opID == "" {
		return nil, fmt.Errorf("monarch: RefreshStatus: operation id required")
	}
	var out struct {
		ForceRefreshOperation *RefreshOperation `json:"forceRefreshOperation"`
	}
	if err := s.c.doGraphQL(ctx, "Common_ForceRefreshOperationQuery", queryRefreshStatus, map[string]any{"id": opID}, &out); err != nil {
		return nil, err
	}
	if out.ForceRefreshOperation == nil {
		return nil, fmt.Errorf("monarch: RefreshStatus: operation %s not found", opID)
	}
	return out.ForceRefreshOperation, nil
}
