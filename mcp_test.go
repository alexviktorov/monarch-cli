package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"monarch-cli/pkg/monarch"
)

// fakeAPI implements monarchAPI for protocol tests.
type fakeAPI struct {
	err       error // returned by every read when set
	tagErr    error // returned by SetTransactionTags only
	updates   []monarch.TransactionUpdate
	updateIDs []string
	tagSets   [][]string
	tagSetIDs []string
}

func (f *fakeAPI) ListAccounts(ctx context.Context) ([]*monarch.Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []*monarch.Account{
		{ID: "a1", DisplayName: "Checking", DisplayBalance: 100, IncludeInNetWorth: true,
			Type: &monarch.AccountType{Name: "depository", Display: "Cash"}},
		{ID: "a2", DisplayName: "Hidden", DisplayBalance: 5, IsHidden: true, IncludeInNetWorth: true},
		// Liability with a positive balance, as the live API reports them:
		// must be subtracted from net worth, never added.
		{ID: "a3", DisplayName: "Visa", DisplayBalance: 30, IncludeInNetWorth: true,
			Type: &monarch.AccountType{Name: "credit", Display: "Credit"}},
	}, nil
}

func (f *fakeAPI) QueryTransactions(ctx context.Context, q txQuery) (*monarch.TransactionList, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &monarch.TransactionList{
		TotalCount: 1,
		Transactions: []*monarch.Transaction{{
			ID: "t1", Amount: -5, PlaidName: "X",
			Category: &monarch.CategoryRef{ID: "c1", Name: "Misc"},
		}},
	}, nil
}

func (f *fakeAPI) GetSummary(ctx context.Context) (*monarch.TransactionSummary, error) {
	return &monarch.TransactionSummary{Count: 1}, f.err
}

func (f *fakeAPI) ListBudgets(ctx context.Context, start, end time.Time) ([]*monarch.BudgetRow, error) {
	return nil, f.err
}

func (f *fakeAPI) GetCashflow(ctx context.Context, start, end time.Time) (*monarch.Cashflow, error) {
	return &monarch.Cashflow{}, f.err
}

func (f *fakeAPI) ListCategories(ctx context.Context) ([]*monarch.Category, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []*monarch.Category{{ID: "c2", Name: "Dining"}}, nil
}

func (f *fakeAPI) ListTags(ctx context.Context) ([]*monarch.Tag, error) {
	return nil, f.err
}

func (f *fakeAPI) ListRecurring(ctx context.Context) ([]*monarch.RecurringItem, error) {
	return nil, f.err
}

func (f *fakeAPI) GetSnapshots(ctx context.Context, p monarch.SnapshotParams) ([]*monarch.AccountSnapshot, error) {
	return nil, f.err
}

func (f *fakeAPI) UpdateTransaction(ctx context.Context, id string, p monarch.TransactionUpdate) error {
	f.updateIDs = append(f.updateIDs, id)
	f.updates = append(f.updates, p)
	return f.err
}

func (f *fakeAPI) SetTransactionTags(ctx context.Context, id string, tagIDs []string) error {
	if f.tagErr != nil {
		return f.tagErr
	}
	f.tagSetIDs = append(f.tagSetIDs, id)
	f.tagSets = append(f.tagSets, tagIDs)
	return f.err
}

func (f *fakeAPI) Ping(ctx context.Context) (*monarch.Identity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &monarch.Identity{ID: "u1", Email: "a@b.c"}, nil
}

func (f *fakeAPI) GetTransaction(ctx context.Context, id string) (*monarch.TransactionDetail, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &monarch.TransactionDetail{Transaction: monarch.Transaction{ID: id, Amount: -5}}, nil
}

func (f *fakeAPI) GetCashflowSummary(ctx context.Context, start, end time.Time) (*monarch.CashflowSummary, error) {
	return &monarch.CashflowSummary{Income: 100}, f.err
}

func (f *fakeAPI) GetDailySnapshots(ctx context.Context, start time.Time) ([]*monarch.DailySnapshot, error) {
	return nil, f.err
}

func (f *fakeAPI) ListHoldings(ctx context.Context, accountID string) ([]*monarch.Holding, error) {
	return nil, f.err
}

func (f *fakeAPI) ListRules(ctx context.Context) ([]*monarch.Rule, error) {
	return nil, f.err
}

func (f *fakeAPI) ListGoals(ctx context.Context) ([]*monarch.Goal, error) {
	return nil, f.err
}

func (f *fakeAPI) ListInstitutions(ctx context.Context) ([]*monarch.Credential, error) {
	return nil, f.err
}

func (f *fakeAPI) CreateTag(ctx context.Context, name, color string) (*monarch.Tag, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &monarch.Tag{ID: "new-tag", Name: name}, nil
}

func startMCP(t *testing.T, api monarchAPI, writes bool, limits *toolLimits) *mcp.ClientSession {
	t.Helper()
	if limits == nil {
		limits = newToolLimits()
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := buildMCPServer(api, writes, logger, limits)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var readToolNames = []string{
	"check_session", "get_accounts", "get_budget", "get_cashflow",
	"get_cashflow_summary", "get_categories", "get_goals", "get_holdings",
	"get_institutions", "get_networth_history", "get_recurring", "get_rules",
	"get_tags", "get_transaction", "get_transaction_summary", "get_transactions",
}

var writeToolNames = []string{"bulk_categorize", "create_tag", "update_transaction"}

func TestMCPToolInventoryReadOnly(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, false, nil)
	got := toolNames(t, cs)
	want := append([]string{}, readToolNames...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tools = %v, want %v", got, want)
		}
	}
}

func TestMCPToolInventoryWithWrites(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, true, nil)
	got := toolNames(t, cs)
	want := append(append([]string{}, readToolNames...), writeToolNames...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tools = %v, want %v", got, want)
		}
	}
}

func TestMCPBulkCategorizeDryRunByDefault(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "bulk_categorize",
		Arguments: map[string]any{
			"transaction_ids": []string{"t1", "t2"},
			"category_id":     "c2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	if len(api.updates) != 0 {
		t.Fatal("dry-run (the default) must not perform any writes")
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out bulkCategorizeOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.DryRun || out.Count != 2 || out.Category.Name != "Dining" || len(out.Planned) != 2 {
		t.Errorf("out = %+v", out)
	}
}

func TestMCPBulkCategorizeExecutes(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "bulk_categorize",
		Arguments: map[string]any{
			"transaction_ids": []string{"t1", "t2"},
			"category_id":     "c2",
			"dry_run":         false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	if len(api.updates) != 2 || *api.updates[0].CategoryID != "c2" {
		t.Errorf("updates = %+v", api.updates)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out bulkCategorizeOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.DryRun || out.Succeeded != 2 || out.Failed != 0 {
		t.Errorf("out = %+v", out)
	}
}

func TestMCPBulkCategorizeRejectsUnknownCategory(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "bulk_categorize",
		Arguments: map[string]any{
			"transaction_ids": []string{"t1"},
			"category_id":     "nope",
			"dry_run":         false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || len(api.updates) != 0 {
		t.Error("unknown category must fail before any write")
	}
}

func TestMCPCreateTag(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_tag", Arguments: map[string]any{"name": "vacations"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out createTagOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "new-tag" || out.Name != "vacations" {
		t.Errorf("out = %+v", out)
	}
}

func TestMCPCheckSession(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, false, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "check_session", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out checkSessionOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Valid || out.Email != "a@b.c" {
		t.Errorf("out = %+v", out)
	}

	bad := startMCP(t, &fakeAPI{err: monarch.ErrSessionExpired}, false, nil)
	res, err = bad.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "check_session", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Valid || !strings.Contains(out.Detail, "monarch login") {
		t.Errorf("out = %+v, want invalid with re-login guidance", out)
	}
}

func TestMCPWriteToolUnknownWhenGated(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, false, nil)
	_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "update_transaction",
		Arguments: map[string]any{"transaction_id": "t1", "notes": "x"},
	})
	if err == nil {
		t.Fatal("calling an unregistered write tool must fail at the protocol level")
	}
}

func TestMCPGetAccounts(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, false, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_accounts", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out getAccountsOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	// Hidden account excluded from the list but counted in net worth;
	// the positive-balance credit account must be SUBTRACTED:
	// assets 105 − liabilities 30 = 75.
	if len(out.Accounts) != 2 || out.Accounts[0].ID != "a1" {
		t.Errorf("accounts = %+v", out.Accounts)
	}
	if out.TotalAssets != 105 || out.TotalLiabilities != 30 || out.NetWorth != 75 {
		t.Errorf("net worth = %+v, want assets 105 − liabilities 30 = 75", out)
	}
	if !out.Accounts[1].IsLiability {
		t.Error("credit account must be flagged is_liability")
	}
}

func TestMCPBadDateIsToolError(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, false, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_transactions", Arguments: map[string]any{"start": "08/01/2026"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("bad date must be a tool error, not a success")
	}
	if txt := resultText(t, res); !strings.Contains(txt, "YYYY-MM-DD") {
		t.Errorf("error text %q should teach the correct format", txt)
	}
}

func TestMCPRedactsSecrets(t *testing.T) {
	api := &fakeAPI{err: errors.New("request failed: Authorization: Token abc123SECRET dropped")}
	cs := startMCP(t, api, false, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_accounts", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error")
	}
	txt := resultText(t, res)
	if strings.Contains(txt, "abc123SECRET") {
		t.Fatalf("secret leaked into tool error: %q", txt)
	}
	if !strings.Contains(txt, "[redacted]") {
		t.Errorf("expected redaction marker in %q", txt)
	}
}

func TestMCPSessionExpiredGuidance(t *testing.T) {
	api := &fakeAPI{err: monarch.ErrSessionExpired}
	cs := startMCP(t, api, false, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_accounts", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt := resultText(t, res)
	if !res.IsError || !strings.Contains(txt, "monarch login") {
		t.Errorf("expired-session error should point at `monarch login`: %q", txt)
	}
}

func TestMCPRateLimit(t *testing.T) {
	limits := &toolLimits{
		bucket:  newTokenBucket(0.0001, 2), // effectively no refill, burst 2
		sem:     make(chan struct{}, 3),
		timeout: 5 * time.Second,
	}
	cs := startMCP(t, &fakeAPI{}, false, limits)
	ctx := context.Background()
	for range 2 {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_accounts", Arguments: map[string]any{}})
		if err != nil || res.IsError {
			t.Fatalf("first calls should pass: %v %v", err, res)
		}
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_accounts", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "rate limited") {
		t.Errorf("third call should be rate limited: %s", resultText(t, res))
	}
}

func TestMCPUpdateTransaction(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "update_transaction",
		Arguments: map[string]any{
			"transaction_id": "t1",
			"category_id":    "c2",
			"tag_ids":        []string{"g1", "g2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	if len(api.updates) != 1 || *api.updates[0].CategoryID != "c2" || api.updates[0].Notes != nil {
		t.Errorf("updates = %+v", api.updates)
	}
	if len(api.tagSets) != 1 || len(api.tagSets[0]) != 2 || api.tagSetIDs[0] != "t1" {
		t.Errorf("tagSets = %+v", api.tagSets)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out updateTransactionOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	want := []string{"category", "tags"}
	if out.ID != "t1" || len(out.Updated) != 2 || out.Updated[0] != want[0] || out.Updated[1] != want[1] {
		t.Errorf("out = %+v, want updated %v", out, want)
	}
}

func TestMCPUpdateTransactionPartialFailure(t *testing.T) {
	api := &fakeAPI{tagErr: errors.New("tag id not found")}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "update_transaction",
		Arguments: map[string]any{
			"transaction_id": "t1",
			"category_id":    "c2",
			"tag_ids":        []string{"bad"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("partial failure must surface as a tool error")
	}
	txt := resultText(t, res)
	if !strings.Contains(txt, "PARTIAL UPDATE") || !strings.Contains(txt, "category") {
		t.Errorf("partial-failure text must state what was applied: %q", txt)
	}
	if len(api.updates) != 1 {
		t.Error("category write should have been attempted and recorded")
	}
}

func TestMCPUpdateTransactionRequiresChange(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "update_transaction",
		Arguments: map[string]any{"transaction_id": "t1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("update with nothing to change must be a tool error")
	}
	if len(api.updates) != 0 || len(api.tagSets) != 0 {
		t.Error("no write must reach the API")
	}
}
