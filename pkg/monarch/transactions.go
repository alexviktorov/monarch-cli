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
	Transactions []*Transaction `json:"transactions"`
	TotalCount   int            `json:"totalCount"`
	// Fetched is the number of rows the server returned for this page,
	// BEFORE the client-side min/max amount filtering. When it differs
	// from len(Transactions), an amount filter dropped rows from this
	// page only — not from the whole result set.
	Fetched    int  `json:"fetched"`
	HasMore    bool `json:"hasMore"`
	NextOffset int  `json:"nextOffset"`
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
	tags           []string
	minAmt, maxAmt *float64
	// Server-side boolean filters (nil = no filter). Key names match the
	// web app's TransactionFilterInput.
	hasNotes        *bool
	hasAttachments  *bool
	isSplit         *bool
	isRecurring     *bool
	hideFromReports *bool
	needsReview     *bool
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
func (q *TransactionQuery) WithTags(ids ...string) *TransactionQuery {
	q.tags = append(q.tags, ids...)
	return q
}
func (q *TransactionQuery) WithHasNotes(v bool) *TransactionQuery { q.hasNotes = &v; return q }
func (q *TransactionQuery) WithHasAttachments(v bool) *TransactionQuery {
	q.hasAttachments = &v
	return q
}
func (q *TransactionQuery) WithIsSplit(v bool) *TransactionQuery     { q.isSplit = &v; return q }
func (q *TransactionQuery) WithIsRecurring(v bool) *TransactionQuery { q.isRecurring = &v; return q }
func (q *TransactionQuery) WithHiddenFromReports(v bool) *TransactionQuery {
	q.hideFromReports = &v
	return q
}

// WithNeedsReview filters to transactions flagged for review. The filter
// key is derived from robcerda/monarch-mcp-server; verify on first live
// use.
func (q *TransactionQuery) WithNeedsReview(v bool) *TransactionQuery { q.needsReview = &v; return q }

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
	if len(q.tags) > 0 {
		filters["tags"] = q.tags
	}
	for key, v := range map[string]*bool{
		"hasNotes":        q.hasNotes,
		"hasAttachments":  q.hasAttachments,
		"isSplit":         q.isSplit,
		"isRecurring":     q.isRecurring,
		"hideFromReports": q.hideFromReports,
		"needsReview":     q.needsReview,
	} {
		if v != nil {
			filters[key] = *v
		}
	}
	// The filters object is always sent, even when empty — the API expects
	// the variable to be present (GetSummary and every known-working client
	// do the same).
	vars := map[string]any{
		"offset":  q.offset,
		"limit":   q.limit,
		"orderBy": "date",
		"filters": filters,
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
	fetched := len(results)
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
		Fetched:      fetched,
		HasMore:      next < out.AllTransactions.TotalCount,
		NextOffset:   next,
	}, nil
}

// TransactionDetail is the full single-transaction view (the web app's
// "drawer"), including split children.
type TransactionDetail struct {
	Transaction
	Pending              bool                `json:"pending"`
	HideFromReports      bool                `json:"hideFromReports"`
	IsRecurring          bool                `json:"isRecurring"`
	NeedsReview          bool                `json:"needsReview"`
	HasSplitTransactions bool                `json:"hasSplitTransactions"`
	IsSplitTransaction   bool                `json:"isSplitTransaction"`
	Splits               []*TransactionSplit `json:"splitTransactions"`
}

type TransactionSplit struct {
	ID       string       `json:"id"`
	Amount   float64      `json:"amount"`
	Notes    string       `json:"notes"`
	Merchant *MerchantRef `json:"merchant"`
	Category *CategoryRef `json:"category"`
}

const queryGetTransaction = `query GetTransactionDetails($id: UUID!) {
  getTransaction(id: $id) {
    id
    amount
    pending
    date
    hideFromReports
    plaidName
    notes
    isRecurring
    needsReview
    hasSplitTransactions
    isSplitTransaction
    category { id name }
    merchant { id name }
    account { id displayName }
    tags { id name }
    splitTransactions {
      id
      amount
      notes
      merchant { id name }
      category { id name }
    }
  }
}`

// Get fetches one transaction with full detail by id.
func (s *TransactionsService) Get(ctx context.Context, id string) (*TransactionDetail, error) {
	if id == "" {
		return nil, fmt.Errorf("monarch: Get: transaction id required")
	}
	var out struct {
		GetTransaction *TransactionDetail `json:"getTransaction"`
	}
	if err := s.c.doGraphQL(ctx, "GetTransactionDetails", queryGetTransaction, map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.GetTransaction == nil {
		return nil, fmt.Errorf("monarch: GetTransactionDetails: transaction %s not found", id)
	}
	return out.GetTransaction, nil
}

// All fetches every matching transaction across pages, up to max (0 means
// 10000), honoring ctx cancellation between pages. The query's offset is
// advanced destructively; treat the builder as consumed afterwards.
func (q *TransactionQuery) All(ctx context.Context, max int) ([]*Transaction, error) {
	if max <= 0 {
		max = 10000
	}
	if q.limit <= 0 || q.limit > 500 {
		q.limit = 500
	}
	var all []*Transaction
	for {
		list, err := q.Execute(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, list.Transactions...)
		if !list.HasMore || len(all) >= max {
			break
		}
		q.offset = list.NextOffset
	}
	if len(all) > max {
		all = all[:max]
	}
	return all, nil
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
