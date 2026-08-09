package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"monarch-cli/pkg/monarch"
)

// monarchAPI is the narrow surface the MCP handlers consume; tests
// substitute a fake.
type monarchAPI interface {
	ListAccounts(ctx context.Context) ([]*monarch.Account, error)
	QueryTransactions(ctx context.Context, q txQuery) (*monarch.TransactionList, error)
	GetSummary(ctx context.Context) (*monarch.TransactionSummary, error)
	ListBudgets(ctx context.Context, start, end time.Time) ([]*monarch.BudgetRow, error)
	GetCashflow(ctx context.Context, start, end time.Time) (*monarch.Cashflow, error)
	ListCategories(ctx context.Context) ([]*monarch.Category, error)
	ListTags(ctx context.Context) ([]*monarch.Tag, error)
	ListRecurring(ctx context.Context) ([]*monarch.RecurringItem, error)
	GetSnapshots(ctx context.Context, p monarch.SnapshotParams) ([]*monarch.AccountSnapshot, error)
	UpdateTransaction(ctx context.Context, id string, p monarch.TransactionUpdate) error
	SetTransactionTags(ctx context.Context, id string, tagIDs []string) error
}

type txQuery struct {
	Start, End           *time.Time
	Search               string
	AccountID            string
	CategoryID           string
	MinAmount, MaxAmount *float64
	Limit, Offset        int
}

// liveAPI adapts *monarch.Client to monarchAPI.
type liveAPI struct{ c *monarch.Client }

func (a *liveAPI) ListAccounts(ctx context.Context) ([]*monarch.Account, error) {
	return a.c.Accounts.List(ctx)
}

func (a *liveAPI) QueryTransactions(ctx context.Context, q txQuery) (*monarch.TransactionList, error) {
	b := a.c.Transactions.Query().Limit(q.Limit).Offset(q.Offset)
	if q.Start != nil && q.End != nil {
		b = b.Between(*q.Start, *q.End)
	}
	if q.Search != "" {
		b = b.Search(q.Search)
	}
	if q.AccountID != "" {
		b = b.WithAccounts(q.AccountID)
	}
	if q.CategoryID != "" {
		b = b.WithCategories(q.CategoryID)
	}
	if q.MinAmount != nil {
		b = b.WithMinAmount(*q.MinAmount)
	}
	if q.MaxAmount != nil {
		b = b.WithMaxAmount(*q.MaxAmount)
	}
	return b.Execute(ctx)
}

func (a *liveAPI) GetSummary(ctx context.Context) (*monarch.TransactionSummary, error) {
	return a.c.Transactions.GetSummary(ctx)
}

func (a *liveAPI) ListBudgets(ctx context.Context, start, end time.Time) ([]*monarch.BudgetRow, error) {
	return a.c.Budgets.List(ctx, start, end)
}

func (a *liveAPI) GetCashflow(ctx context.Context, start, end time.Time) (*monarch.Cashflow, error) {
	return a.c.Cashflow.Get(ctx, monarch.CashflowParams{StartDate: start, EndDate: end})
}

func (a *liveAPI) ListCategories(ctx context.Context) ([]*monarch.Category, error) {
	return a.c.Categories.List(ctx)
}

func (a *liveAPI) ListTags(ctx context.Context) ([]*monarch.Tag, error) {
	return a.c.Tags.List(ctx)
}

func (a *liveAPI) ListRecurring(ctx context.Context) ([]*monarch.RecurringItem, error) {
	return a.c.Recurring.List(ctx)
}

func (a *liveAPI) GetSnapshots(ctx context.Context, p monarch.SnapshotParams) ([]*monarch.AccountSnapshot, error) {
	return a.c.Accounts.GetSnapshots(ctx, p)
}

func (a *liveAPI) UpdateTransaction(ctx context.Context, id string, p monarch.TransactionUpdate) error {
	_, err := a.c.Transactions.Update(ctx, id, p)
	return err
}

func (a *liveAPI) SetTransactionTags(ctx context.Context, id string, tagIDs []string) error {
	_, err := a.c.Transactions.SetTags(ctx, id, tagIDs)
	return err
}

// ---- shared output shapes ----

type refOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func categoryRef(c *monarch.CategoryRef) *refOut {
	if c == nil {
		return nil
	}
	return &refOut{ID: c.ID, Name: c.Name}
}

const serverInstructions = "Read access to the owner's Monarch Money data. " +
	"Amounts follow Monarch's sign convention: expenses negative, income positive " +
	"(budget spent and cashflow expenses are reported positive). " +
	"Read tools return stable ids; write tools accept ids only — resolve names via " +
	"get_categories / get_tags / get_accounts / get_transactions first. Dates are YYYY-MM-DD."

func parseToolDate(s, name string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q: use YYYY-MM-DD", name, s)
	}
	return t, nil
}

type toolHandlers struct {
	api    monarchAPI
	writes bool
	logger *slog.Logger
}

// ---- get_accounts ----

type getAccountsIn struct {
	IncludeHidden bool `json:"include_hidden,omitempty" jsonschema:"include accounts hidden in Monarch (default false)"`
}

type accountOut struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type,omitempty"`
	Institution string  `json:"institution,omitempty"`
	Balance     float64 `json:"balance"`
	Hidden      bool    `json:"hidden,omitempty"`
	InNetWorth  bool    `json:"in_net_worth"`
	Updated     string  `json:"updated,omitempty"`
}

type getAccountsOut struct {
	Accounts []accountOut `json:"accounts"`
	NetWorth float64      `json:"net_worth"`
}

func (h *toolHandlers) getAccounts(ctx context.Context, req *mcp.CallToolRequest, in getAccountsIn) (*mcp.CallToolResult, getAccountsOut, error) {
	accounts, err := h.api.ListAccounts(ctx)
	if err != nil {
		return nil, getAccountsOut{}, err
	}
	out := getAccountsOut{Accounts: []accountOut{}}
	for _, a := range accounts {
		if a.IncludeInNetWorth {
			out.NetWorth += a.DisplayBalance
		}
		if a.IsHidden && !in.IncludeHidden {
			continue
		}
		o := accountOut{
			ID: a.ID, Name: a.DisplayName, Balance: a.DisplayBalance,
			Hidden: a.IsHidden, InNetWorth: a.IncludeInNetWorth,
		}
		if a.Type != nil {
			o.Type = a.Type.Display
		}
		if a.Institution != nil {
			o.Institution = a.Institution.Name
		}
		if !a.DisplayLastUpdatedAt.IsZero() {
			o.Updated = a.DisplayLastUpdatedAt.Format("2006-01-02")
		}
		out.Accounts = append(out.Accounts, o)
	}
	return nil, out, nil
}

// ---- get_transactions ----

type getTransactionsIn struct {
	Start      string   `json:"start,omitempty" jsonschema:"start date YYYY-MM-DD"`
	End        string   `json:"end,omitempty" jsonschema:"end date YYYY-MM-DD"`
	Search     string   `json:"search,omitempty" jsonschema:"free-text search over merchant and notes"`
	AccountID  string   `json:"account_id,omitempty" jsonschema:"filter by account id from get_accounts"`
	CategoryID string   `json:"category_id,omitempty" jsonschema:"filter by category id from get_categories"`
	MinAmount  *float64 `json:"min_amount,omitempty" jsonschema:"minimum absolute amount"`
	MaxAmount  *float64 `json:"max_amount,omitempty" jsonschema:"maximum absolute amount"`
	Limit      int      `json:"limit,omitempty" jsonschema:"max results (default 50, cap 500)"`
	Offset     int      `json:"offset,omitempty" jsonschema:"pagination offset"`
}

type txOut struct {
	ID       string   `json:"id"`
	Date     string   `json:"date"`
	Amount   float64  `json:"amount"`
	Merchant string   `json:"merchant,omitempty"`
	Category *refOut  `json:"category,omitempty"`
	Account  *refOut  `json:"account,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	Tags     []refOut `json:"tags,omitempty"`
}

type getTransactionsOut struct {
	Total        int     `json:"total"`
	Count        int     `json:"count"`
	Offset       int     `json:"offset"`
	HasMore      bool    `json:"has_more"`
	NextOffset   int     `json:"next_offset,omitempty"`
	Transactions []txOut `json:"transactions"`
}

func (h *toolHandlers) getTransactions(ctx context.Context, req *mcp.CallToolRequest, in getTransactionsIn) (*mcp.CallToolResult, getTransactionsOut, error) {
	var zero getTransactionsOut
	q := txQuery{
		Search:     in.Search,
		AccountID:  in.AccountID,
		CategoryID: in.CategoryID,
		MinAmount:  in.MinAmount,
		MaxAmount:  in.MaxAmount,
		Limit:      in.Limit,
		Offset:     in.Offset,
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	q.Limit = min(q.Limit, 500)
	if in.Start != "" || in.End != "" {
		start, end := "1970-01-01", time.Now().Format("2006-01-02")
		if in.Start != "" {
			start = in.Start
		}
		if in.End != "" {
			end = in.End
		}
		s, err := parseToolDate(start, "start")
		if err != nil {
			return nil, zero, err
		}
		e, err := parseToolDate(end, "end")
		if err != nil {
			return nil, zero, err
		}
		q.Start, q.End = &s, &e
	}
	list, err := h.api.QueryTransactions(ctx, q)
	if err != nil {
		return nil, zero, err
	}
	out := getTransactionsOut{
		Total: list.TotalCount, Count: len(list.Transactions),
		Offset: q.Offset, HasMore: list.HasMore,
		Transactions: []txOut{},
	}
	if list.HasMore {
		out.NextOffset = list.NextOffset
	}
	for _, tx := range list.Transactions {
		o := txOut{
			ID: tx.ID, Date: tx.Date.Format("2006-01-02"), Amount: tx.Amount,
			Merchant: tx.PlaidName, Notes: tx.Notes,
			Category: categoryRef(tx.Category),
		}
		if tx.Merchant != nil && tx.Merchant.Name != "" {
			o.Merchant = tx.Merchant.Name
		}
		if tx.Account != nil {
			o.Account = &refOut{ID: tx.Account.ID, Name: tx.Account.DisplayName}
		}
		for _, tag := range tx.Tags {
			o.Tags = append(o.Tags, refOut{ID: tag.ID, Name: tag.Name})
		}
		out.Transactions = append(out.Transactions, o)
	}
	return nil, out, nil
}

// ---- get_transaction_summary ----

type summaryOut struct {
	Count          int     `json:"count"`
	FirstDate      string  `json:"first_date"`
	LastDate       string  `json:"last_date"`
	TotalIncome    float64 `json:"total_income"`
	TotalExpense   float64 `json:"total_expense"`
	Average        float64 `json:"average"`
	LargestExpense float64 `json:"largest_expense"`
}

func (h *toolHandlers) getSummary(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, summaryOut, error) {
	s, err := h.api.GetSummary(ctx)
	if err != nil {
		return nil, summaryOut{}, err
	}
	return nil, summaryOut{
		Count: s.Count, FirstDate: s.First, LastDate: s.Last,
		TotalIncome: s.SumIncome, TotalExpense: s.SumExpense,
		Average: s.Avg, LargestExpense: s.MaxExpense,
	}, nil
}

// ---- get_budget ----

type getBudgetIn struct {
	Month string `json:"month,omitempty" jsonschema:"budget month YYYY-MM (default: current month)"`
}

type budgetRowOut struct {
	CategoryID string  `json:"category_id"`
	Category   string  `json:"category"`
	Budgeted   float64 `json:"budgeted"`
	Spent      float64 `json:"spent"`
	Remaining  float64 `json:"remaining"`
}

type getBudgetOut struct {
	Month         string         `json:"month"`
	Rows          []budgetRowOut `json:"rows"`
	TotalBudgeted float64        `json:"total_budgeted"`
	TotalSpent    float64        `json:"total_spent"`
}

func (h *toolHandlers) getBudget(ctx context.Context, req *mcp.CallToolRequest, in getBudgetIn) (*mcp.CallToolResult, getBudgetOut, error) {
	var zero getBudgetOut
	now := time.Now()
	if in.Month != "" {
		m, err := time.Parse("2006-01", in.Month)
		if err != nil {
			return nil, zero, fmt.Errorf("invalid month %q: use YYYY-MM", in.Month)
		}
		now = m
	}
	start, end := monthRange(now)
	rows, err := h.api.ListBudgets(ctx, start, end)
	if err != nil {
		return nil, zero, err
	}
	out := getBudgetOut{Month: start.Format("2006-01"), Rows: []budgetRowOut{}}
	for _, b := range rows {
		name := b.CategoryID
		if b.Category != nil {
			name = b.Category.Name
		}
		out.TotalBudgeted += b.Amount
		out.TotalSpent += b.Spent
		out.Rows = append(out.Rows, budgetRowOut{
			CategoryID: b.CategoryID, Category: name,
			Budgeted: b.Amount, Spent: b.Spent, Remaining: b.Remaining,
		})
	}
	return nil, out, nil
}

// ---- get_cashflow ----

type getCashflowIn struct {
	Start string `json:"start,omitempty" jsonschema:"start date YYYY-MM-DD (default: first of current month)"`
	End   string `json:"end,omitempty" jsonschema:"end date YYYY-MM-DD (default: today)"`
}

type cashflowGroupOut struct {
	ID     string  `json:"id,omitempty"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

type getCashflowOut struct {
	Start       string             `json:"start"`
	End         string             `json:"end"`
	Income      float64            `json:"income"`
	Expenses    float64            `json:"expenses"`
	Savings     float64            `json:"savings"`
	SavingsRate float64            `json:"savings_rate"`
	ByCategory  []cashflowGroupOut `json:"by_category"`
	ByMerchant  []cashflowGroupOut `json:"by_merchant"`
}

func (h *toolHandlers) getCashflow(ctx context.Context, req *mcp.CallToolRequest, in getCashflowIn) (*mcp.CallToolResult, getCashflowOut, error) {
	var zero getCashflowOut
	start, end := monthRange(time.Now())
	end = time.Now()
	var err error
	if in.Start != "" {
		if start, err = parseToolDate(in.Start, "start"); err != nil {
			return nil, zero, err
		}
	}
	if in.End != "" {
		if end, err = parseToolDate(in.End, "end"); err != nil {
			return nil, zero, err
		}
	}
	cf, err := h.api.GetCashflow(ctx, start, end)
	if err != nil {
		return nil, zero, err
	}
	out := getCashflowOut{
		Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"),
		ByCategory: []cashflowGroupOut{}, ByMerchant: []cashflowGroupOut{},
	}
	if cf.Summary != nil {
		out.Income = cf.Summary.Income
		out.Expenses = cf.Summary.Expense
		out.Savings = cf.Summary.Savings
		out.SavingsRate = cf.Summary.SavingsRate
	}
	for _, c := range cf.ByCategory {
		g := cashflowGroupOut{Amount: c.Amount}
		if c.Category != nil {
			g.ID, g.Name = c.Category.ID, c.Category.Name
		}
		out.ByCategory = append(out.ByCategory, g)
	}
	for _, m := range cf.ByMerchant {
		g := cashflowGroupOut{Amount: m.Amount}
		if m.Merchant != nil {
			g.ID, g.Name = m.Merchant.ID, m.Merchant.Name
		}
		out.ByMerchant = append(out.ByMerchant, g)
	}
	return nil, out, nil
}

// ---- get_categories ----

type categoryOut struct {
	ID    string `json:"id"`
	Group string `json:"group,omitempty"`
	Name  string `json:"name"`
}

type getCategoriesOut struct {
	Categories []categoryOut `json:"categories"`
}

func (h *toolHandlers) getCategories(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, getCategoriesOut, error) {
	cats, err := h.api.ListCategories(ctx)
	if err != nil {
		return nil, getCategoriesOut{}, err
	}
	out := getCategoriesOut{Categories: []categoryOut{}}
	for _, c := range cats {
		if c.IsDisabled {
			continue
		}
		o := categoryOut{ID: c.ID, Name: c.Name}
		if c.Group != nil {
			o.Group = c.Group.Name
		}
		out.Categories = append(out.Categories, o)
	}
	return nil, out, nil
}

// ---- get_tags ----

type getTagsOut struct {
	Tags []refOut `json:"tags"`
}

func (h *toolHandlers) getTags(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, getTagsOut, error) {
	tags, err := h.api.ListTags(ctx)
	if err != nil {
		return nil, getTagsOut{}, err
	}
	out := getTagsOut{Tags: []refOut{}}
	for _, tag := range tags {
		out.Tags = append(out.Tags, refOut{ID: tag.ID, Name: tag.Name})
	}
	return nil, out, nil
}

// ---- get_recurring ----

type recurringItemOut struct {
	Merchant  string  `json:"merchant,omitempty"`
	Amount    float64 `json:"amount"`
	Frequency string  `json:"frequency"`
	NextDate  string  `json:"next_date"`
	Category  string  `json:"category,omitempty"`
}

type getRecurringOut struct {
	Items            []recurringItemOut `json:"items"`
	EstimatedMonthly float64            `json:"estimated_monthly_spend"`
}

func (h *toolHandlers) getRecurring(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, getRecurringOut, error) {
	recs, err := h.api.ListRecurring(ctx)
	if err != nil {
		return nil, getRecurringOut{}, err
	}
	out := getRecurringOut{Items: []recurringItemOut{}, EstimatedMonthly: monthlyEstimate(recs)}
	for _, r := range recs {
		o := recurringItemOut{
			Amount: r.Amount, Frequency: r.Frequency,
			NextDate: r.NextDate.Format("2006-01-02"),
		}
		if r.Merchant != nil {
			o.Merchant = r.Merchant.Name
		}
		if r.Category != nil {
			o.Category = r.Category.Name
		}
		out.Items = append(out.Items, o)
	}
	return nil, out, nil
}

// ---- get_networth_history ----

type getNetworthIn struct {
	Start     string `json:"start,omitempty" jsonschema:"start date YYYY-MM-DD (default: 12 months ago)"`
	Timeframe string `json:"timeframe,omitempty" jsonschema:"granularity: month or year (default month)"`
}

type networthPointOut struct {
	Period      string  `json:"period"`
	Assets      float64 `json:"assets"`
	Liabilities float64 `json:"liabilities"`
	NetWorth    float64 `json:"net_worth"`
}

type getNetworthOut struct {
	Points []networthPointOut `json:"points"`
}

func (h *toolHandlers) getNetworth(ctx context.Context, req *mcp.CallToolRequest, in getNetworthIn) (*mcp.CallToolResult, getNetworthOut, error) {
	var zero getNetworthOut
	start := time.Now().AddDate(-1, 0, 0)
	var err error
	if in.Start != "" {
		if start, err = parseToolDate(in.Start, "start"); err != nil {
			return nil, zero, err
		}
	}
	timeframe := in.Timeframe
	if timeframe == "" {
		timeframe = "month"
	}
	snaps, err := h.api.GetSnapshots(ctx, monarch.SnapshotParams{StartDate: start, Timeframe: timeframe})
	if err != nil {
		return nil, zero, err
	}
	out := getNetworthOut{Points: []networthPointOut{}}
	for _, p := range networthPoints(snaps) {
		out.Points = append(out.Points, networthPointOut{
			Period: p.Period, Assets: p.Assets,
			Liabilities: p.Liabilities, NetWorth: p.NetWorth,
		})
	}
	return nil, out, nil
}

// ---- update_transaction (write; registered only when gated on) ----

type updateTransactionIn struct {
	TransactionID string    `json:"transaction_id" jsonschema:"id from get_transactions"`
	CategoryID    *string   `json:"category_id,omitempty" jsonschema:"new category id from get_categories; omit to leave unchanged"`
	Notes         *string   `json:"notes,omitempty" jsonschema:"replacement notes; empty string clears; omit to leave unchanged"`
	TagIDs        *[]string `json:"tag_ids,omitempty" jsonschema:"full replacement set of tag ids from get_tags; empty array clears all; omit to leave unchanged"`
}

type updateTransactionOut struct {
	ID      string   `json:"id"`
	Updated []string `json:"updated"`
}

func (h *toolHandlers) updateTransaction(ctx context.Context, req *mcp.CallToolRequest, in updateTransactionIn) (*mcp.CallToolResult, updateTransactionOut, error) {
	var zero updateTransactionOut
	// Defense in depth: the tool is only registered when writes are on,
	// but re-check in case a future refactor breaks that invariant.
	if !h.writes {
		return nil, zero, errors.New("write tools are disabled on this server")
	}
	if in.TransactionID == "" {
		return nil, zero, errors.New("transaction_id is required")
	}
	if in.CategoryID == nil && in.Notes == nil && in.TagIDs == nil {
		return nil, zero, errors.New("nothing to change: provide category_id, notes, and/or tag_ids")
	}
	out := updateTransactionOut{ID: in.TransactionID, Updated: []string{}}
	if in.CategoryID != nil || in.Notes != nil {
		err := h.api.UpdateTransaction(ctx, in.TransactionID, monarch.TransactionUpdate{
			CategoryID: in.CategoryID,
			Notes:      in.Notes,
		})
		if err != nil {
			return nil, zero, err
		}
		if in.CategoryID != nil {
			out.Updated = append(out.Updated, "category")
		}
		if in.Notes != nil {
			out.Updated = append(out.Updated, "notes")
		}
	}
	if in.TagIDs != nil {
		if err := h.api.SetTransactionTags(ctx, in.TransactionID, *in.TagIDs); err != nil {
			return nil, zero, err
		}
		out.Updated = append(out.Updated, "tags")
	}
	// Field names only — never values (notes could hold anything).
	h.logger.Info("write applied", "tool", "update_transaction",
		"transaction_id", in.TransactionID, "fields", out.Updated)
	return nil, out, nil
}

// ---- server construction ----

func ptr[T any](v T) *T { return &v }

func roAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)}
}

func buildMCPServer(api monarchAPI, writes bool, logger *slog.Logger, limits *toolLimits) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "monarch", Version: appVersion},
		&mcp.ServerOptions{Instructions: serverInstructions, Logger: logger},
	)
	h := &toolHandlers{api: api, writes: writes, logger: logger}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_accounts",
		Description: "List accounts with balances, institution, and stable account ids; includes computed net worth over included accounts.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getAccounts))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_transactions",
		Description: "Search transactions with date/search/account/category/amount filters. Returns stable transaction ids for use with update_transaction.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getTransactions))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_transaction_summary",
		Description: "Lifetime transaction statistics: count, date range, total income/expense, average, largest expense.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getSummary))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_budget",
		Description: "Budget vs. actual per category for one month (budgeted and spent are positive numbers).",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getBudget))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_cashflow",
		Description: "Income, expenses, savings rate, and per-category/per-merchant sums for a date range (category/merchant sums are signed: negative = spending).",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getCashflow))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_categories",
		Description: "List transaction categories with stable ids — use these ids for get_transactions.category_id and update_transaction.category_id.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getCategories))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_tags",
		Description: "List transaction tags with stable ids — use these ids for update_transaction.tag_ids.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getTags))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_recurring",
		Description: "Upcoming recurring bills/subscriptions with a normalized estimated monthly spend.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getRecurring))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_networth_history",
		Description: "Net worth history: per-period assets, liabilities, and net worth.",
		Annotations: roAnnotations(),
	}, wrap(limits, h.getNetworth))

	if writes {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "update_transaction",
			Description: "Update one transaction: category and/or notes and/or full tag set. Omitted fields stay unchanged; empty notes/tag list clears. IDs come from the read tools.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), IdempotentHint: true, OpenWorldHint: ptr(false)},
		}, wrap(limits, h.updateTransaction))
	}
	return server
}
