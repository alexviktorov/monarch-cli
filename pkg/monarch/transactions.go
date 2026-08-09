package monarch

import (
	"context"
	"fmt"
	"math"
	"time"
)

type TransactionsService struct{ c *Client }

type Transaction struct {
	ID        string       `json:"id"`
	Date      Date         `json:"date"`
	Amount    float64      `json:"amount"`
	PlaidName string       `json:"plaidName"`
	Notes     string       `json:"notes"`
	Merchant  *MerchantRef `json:"merchant"`
	Category  *CategoryRef `json:"category"`
	Account   *AccountRef  `json:"account"`
	Tags      []*Tag       `json:"tags"`
}

type TransactionList struct {
	Transactions []*Transaction
	TotalCount   int
	HasMore      bool
	NextOffset   int
}

const queryGetTransactionsList = `query GetTransactionsList($offset: Int, $limit: Int, $filters: TransactionFilterInput, $orderBy: TransactionOrdering) {
  allTransactions(filters: $filters) {
    totalCount
    results(offset: $offset, limit: $limit, orderBy: $orderBy) {
      id
      date
      amount
      plaidName
      notes
      merchant { id name }
      category { id name }
      account { id displayName }
      tags { id name }
    }
  }
}`

// Query starts a transaction search. Defaults: limit 50, offset 0, newest
// first.
func (s *TransactionsService) Query() *TransactionQuery {
	return &TransactionQuery{s: s, limit: 50}
}

type TransactionQuery struct {
	s              *TransactionsService
	limit, offset  int
	start, end     *time.Time
	search         string
	accounts       []string
	categories     []string
	minAmt, maxAmt *float64
}

func (q *TransactionQuery) Limit(n int) *TransactionQuery  { q.limit = n; return q }
func (q *TransactionQuery) Offset(n int) *TransactionQuery { q.offset = n; return q }
func (q *TransactionQuery) Between(start, end time.Time) *TransactionQuery {
	q.start, q.end = &start, &end
	return q
}
func (q *TransactionQuery) Search(text string) *TransactionQuery { q.search = text; return q }
func (q *TransactionQuery) WithAccounts(ids ...string) *TransactionQuery {
	q.accounts = append(q.accounts, ids...)
	return q
}
func (q *TransactionQuery) WithCategories(ids ...string) *TransactionQuery {
	q.categories = append(q.categories, ids...)
	return q
}

// WithMinAmount / WithMaxAmount bound the transaction's absolute value.
// The server's filter input has no amount fields, so these are applied
// client-side after the page is fetched; TotalCount remains the server's
// unfiltered count.
func (q *TransactionQuery) WithMinAmount(v float64) *TransactionQuery { q.minAmt = &v; return q }
func (q *TransactionQuery) WithMaxAmount(v float64) *TransactionQuery { q.maxAmt = &v; return q }

func (q *TransactionQuery) Execute(ctx context.Context) (*TransactionList, error) {
	filters := map[string]any{}
	if q.start != nil {
		filters["startDate"] = q.start.Format("2006-01-02")
	}
	if q.end != nil {
		filters["endDate"] = q.end.Format("2006-01-02")
	}
	if q.search != "" {
		filters["search"] = q.search
	}
	if len(q.accounts) > 0 {
		filters["accounts"] = q.accounts
	}
	if len(q.categories) > 0 {
		filters["categories"] = q.categories
	}
	vars := map[string]any{
		"offset":  q.offset,
		"limit":   q.limit,
		"orderBy": "date",
	}
	if len(filters) > 0 {
		vars["filters"] = filters
	}
	var out struct {
		AllTransactions struct {
			TotalCount int            `json:"totalCount"`
			Results    []*Transaction `json:"results"`
		} `json:"allTransactions"`
	}
	if err := q.s.c.doGraphQL(ctx, "GetTransactionsList", queryGetTransactionsList, vars, &out); err != nil {
		return nil, err
	}
	results := out.AllTransactions.Results
	if q.minAmt != nil || q.maxAmt != nil {
		kept := make([]*Transaction, 0, len(results))
		for _, tx := range results {
			abs := math.Abs(tx.Amount)
			if q.minAmt != nil && abs < *q.minAmt {
				continue
			}
			if q.maxAmt != nil && abs > *q.maxAmt {
				continue
			}
			kept = append(kept, tx)
		}
		results = kept
	}
	next := q.offset + q.limit
	return &TransactionList{
		Transactions: results,
		TotalCount:   out.AllTransactions.TotalCount,
		HasMore:      next < out.AllTransactions.TotalCount,
		NextOffset:   next,
	}, nil
}

type TransactionSummary struct {
	Count      int     `json:"count"`
	First      string  `json:"first"`
	Last       string  `json:"last"`
	Avg        float64 `json:"avg"`
	MaxExpense float64 `json:"maxExpense"`
	SumIncome  float64 `json:"sumIncome"`
	SumExpense float64 `json:"sumExpense"`
}

const queryGetTransactionsPage = `query GetTransactionsPage($filters: TransactionFilterInput) {
  aggregates(filters: $filters) {
    summary {
      avg
      count
      maxExpense
      sumIncome
      sumExpense
      first
      last
    }
  }
}`

func (s *TransactionsService) GetSummary(ctx context.Context) (*TransactionSummary, error) {
	// aggregates is an array; an empty filters object selects everything.
	vars := map[string]any{"filters": map[string]any{}}
	var out struct {
		Aggregates []struct {
			Summary *TransactionSummary `json:"summary"`
		} `json:"aggregates"`
	}
	if err := s.c.doGraphQL(ctx, "GetTransactionsPage", queryGetTransactionsPage, vars, &out); err != nil {
		return nil, err
	}
	if len(out.Aggregates) == 0 || out.Aggregates[0].Summary == nil {
		return nil, fmt.Errorf("monarch: GetTransactionsPage: empty aggregates in response")
	}
	return out.Aggregates[0].Summary, nil
}
