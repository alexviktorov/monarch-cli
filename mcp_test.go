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
	err             error // returned by every method when set
	tagErr          error // returned by SetTransactionTags only
	updates         []monarch.TransactionUpdate
	updateIDs       []string
	tagSets         [][]string
	tagSetIDs       []string
	splitsSet       [][]monarch.SplitInput
	txCreates       []monarch.CreateTransactionParams
	budgetSets      []monarch.BudgetItemParams
	merchantUpdates []monarch.MerchantUpdate
	catCreates      []monarch.CategoryCreate
	catUpdates      []monarch.CategoryUpdate
	catDeletes      [][2]string
	ruleCreates     []monarch.RuleInput
	ruleUpdates     []monarch.RuleInput
	ruleDeletes     []string
	rulesSeq        [][]*monarch.Rule // successive ListRules responses
	txDeletes       []string
	tagUpdates      []string
	tagDeletes      []string
	merges          [][2]string
	goalMoves       [][3]string
	refreshed       bool
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
	if f.err != nil {
		return nil, f.err
	}
	if len(f.rulesSeq) > 0 {
		next := f.rulesSeq[0]
		f.rulesSeq = f.rulesSeq[1:]
		return next, nil
	}
	return nil, nil
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

func (f *fakeAPI) SetTransactionSplits(ctx context.Context, id string, splits []monarch.SplitInput) (*monarch.SplitResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.splitsSet = append(f.splitsSet, splits)
	return &monarch.SplitResult{TransactionID: id, HasSplitTransactions: len(splits) > 0}, nil
}

func (f *fakeAPI) CreateTransaction(ctx context.Context, p monarch.CreateTransactionParams) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.txCreates = append(f.txCreates, p)
	return "created-tx", nil
}

func (f *fakeAPI) SetBudgetAmount(ctx context.Context, p monarch.BudgetItemParams) (*monarch.BudgetItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.budgetSets = append(f.budgetSets, p)
	return &monarch.BudgetItem{ID: "b1", BudgetAmount: p.Amount}, nil
}

func (f *fakeAPI) ListMerchants(ctx context.Context, search string, limit int) ([]*monarch.Merchant, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []*monarch.Merchant{
		{ID: "m1", Name: "Netflix", TransactionCount: 12},
		{ID: "m2", Name: "Netflix Inc", TransactionCount: 3},
	}, nil
}

func (f *fakeAPI) UpdateMerchant(ctx context.Context, id string, p monarch.MerchantUpdate) (*monarch.MerchantInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.merchantUpdates = append(f.merchantUpdates, p)
	return &monarch.MerchantInfo{ID: id, Name: "Renamed"}, nil
}

func (f *fakeAPI) CreateCategory(ctx context.Context, p monarch.CategoryCreate) (*monarch.Category, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.catCreates = append(f.catCreates, p)
	return &monarch.Category{ID: "new-cat", Name: p.Name}, nil
}

func (f *fakeAPI) UpdateCategory(ctx context.Context, id string, p monarch.CategoryUpdate) (*monarch.Category, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.catUpdates = append(f.catUpdates, p)
	return &monarch.Category{ID: id, Name: "Renamed"}, nil
}

func (f *fakeAPI) DeleteCategory(ctx context.Context, id, moveTo string) error {
	if f.err != nil {
		return f.err
	}
	f.catDeletes = append(f.catDeletes, [2]string{id, moveTo})
	return nil
}

func (f *fakeAPI) CreateRule(ctx context.Context, r monarch.RuleInput) error {
	if f.err != nil {
		return f.err
	}
	f.ruleCreates = append(f.ruleCreates, r)
	return nil
}

func (f *fakeAPI) UpdateRule(ctx context.Context, id string, r monarch.RuleInput) error {
	if f.err != nil {
		return f.err
	}
	f.ruleUpdates = append(f.ruleUpdates, r)
	return nil
}

func (f *fakeAPI) DeleteTransaction(ctx context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.txDeletes = append(f.txDeletes, id)
	return nil
}

func (f *fakeAPI) UpdateTag(ctx context.Context, id string, p monarch.TagUpdate) (*monarch.Tag, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.tagUpdates = append(f.tagUpdates, id)
	return &monarch.Tag{ID: id, Name: "renamed"}, nil
}

func (f *fakeAPI) DeleteTag(ctx context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.tagDeletes = append(f.tagDeletes, id)
	return nil
}

func (f *fakeAPI) MergeMerchants(ctx context.Context, dup, target string) error {
	if f.err != nil {
		return f.err
	}
	f.merges = append(f.merges, [2]string{dup, target})
	return nil
}

func (f *fakeAPI) GetCreditScoreHistory(ctx context.Context) ([]*monarch.CreditScoreSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []*monarch.CreditScoreSnapshot{{ReportedDate: "2026-08-01", Score: 780}}, nil
}

func (f *fakeAPI) ForceRefreshAccounts(ctx context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.refreshed = true
	return "op-1", nil
}

func (f *fakeAPI) RefreshAccountsStatus(ctx context.Context, opID string) (*monarch.RefreshOperation, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &monarch.RefreshOperation{ID: opID, State: "COMPLETED", Completed: 3, Total: 3}, nil
}

func (f *fakeAPI) ContributeToGoal(ctx context.Context, goalID, accountID string, amount float64) (*monarch.GoalMoveResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.goalMoves = append(f.goalMoves, [3]string{"contribute", goalID, accountID})
	return &monarch.GoalMoveResult{GoalID: goalID, CurrentBalance: 100 + amount, EventID: "ev1"}, nil
}

func (f *fakeAPI) WithdrawFromGoal(ctx context.Context, goalID, accountID string, amount float64) (*monarch.GoalMoveResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.goalMoves = append(f.goalMoves, [3]string{"withdraw", goalID, accountID})
	return &monarch.GoalMoveResult{GoalID: goalID, CurrentBalance: 100 - amount, EventID: "ev2"}, nil
}

func (f *fakeAPI) DeleteRule(ctx context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.ruleDeletes = append(f.ruleDeletes, id)
	return nil
}

func startMCP(t *testing.T, api monarchAPI, writes bool, limits *toolLimits) *mcp.ClientSession {
	// Default: an already-logged-in fake auth and a plain client.
	return startMCPWith(t, api, &fakeAuth{email: "a@b.c"}, writes, limits, nil)
}

// startMCPWith allows injecting an authController and a client
// ElicitationHandler (for the login flow).
func startMCPWith(t *testing.T, api monarchAPI, auth authController, writes bool, limits *toolLimits,
	elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) *mcp.ClientSession {
	t.Helper()
	if limits == nil {
		limits = newToolLimits()
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := buildMCPServer(api, auth, writes, logger, limits)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	var copts *mcp.ClientOptions
	if elicit != nil {
		copts = &mcp.ClientOptions{ElicitationHandler: elicit}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "0"}, copts)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// fakeAuth records login calls and lets a test script the challenge flow.
type fakeAuth struct {
	email     string   // current session email ("" = not logged in)
	challenge error    // returned by Login to trigger MFA/OTP/captcha
	calls     []string // "login", "mfa", "otp"
}

func (a *fakeAuth) SessionEmail() string { return a.email }
func (a *fakeAuth) Login(ctx context.Context, email, password string) error {
	a.calls = append(a.calls, "login")
	if a.challenge != nil {
		return a.challenge
	}
	a.email = email
	return nil
}
func (a *fakeAuth) LoginWithMFA(ctx context.Context, email, password, code string) error {
	a.calls = append(a.calls, "mfa")
	a.email = email
	return nil
}
func (a *fakeAuth) LoginWithEmailOTP(ctx context.Context, email, password, code string) error {
	a.calls = append(a.calls, "otp")
	a.email = email
	return nil
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
	"get_institutions", "get_merchants", "get_networth_history", "get_recurring",
	"get_credit_score", "get_rules", "get_tags", "get_transaction",
	"get_transaction_summary", "get_transactions", "monarch_login",
	"refresh_status",
}

var _ = 0 // inventory below includes contribute/withdraw goal writes

var writeToolNames = []string{
	"bulk_categorize", "bulk_update_transactions", "contribute_to_goal",
	"create_category", "create_rule", "create_tag", "create_transaction",
	"delete_category", "delete_rule", "delete_tag", "delete_transaction",
	"merge_merchants", "refresh_accounts", "set_budget_amount",
	"set_transaction_splits", "update_category", "update_merchant",
	"update_rule", "update_tag", "update_transaction", "withdraw_from_goal",
}

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

func TestMCPDeletesRequireConfirm(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	for tool, args := range map[string]map[string]any{
		"delete_category": {"category_id": "c1"},
		"delete_rule":     {"rule_id": "r1"},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
			t.Errorf("%s without confirm must refuse: %s", tool, resultText(t, res))
		}
	}
	if len(api.catDeletes) != 0 || len(api.ruleDeletes) != 0 {
		t.Error("no delete may reach the API without confirm")
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "delete_category", Arguments: map[string]any{"category_id": "c1", "move_to_category_id": "c2", "confirm": true},
	})
	if err != nil || res.IsError {
		t.Fatalf("confirmed delete failed: %v %s", err, resultText(t, res))
	}
	if len(api.catDeletes) != 1 || api.catDeletes[0] != [2]string{"c1", "c2"} {
		t.Errorf("catDeletes = %v", api.catDeletes)
	}
}

func TestMCPSplitsSumGuard(t *testing.T) {
	api := &fakeAPI{} // fake GetTransaction returns Amount -5
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "set_transaction_splits",
		Arguments: map[string]any{
			"transaction_id": "t1",
			"splits":         []map[string]any{{"amount": -3}, {"amount": -1}}, // sums to -4, parent is -5
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "-5.00") {
		t.Errorf("mismatched sum must be rejected with the expected total: %s", resultText(t, res))
	}
	if len(api.splitsSet) != 0 {
		t.Error("no write may happen on sum mismatch")
	}
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "set_transaction_splits",
		Arguments: map[string]any{
			"transaction_id": "t1",
			"splits":         []map[string]any{{"amount": -3, "category_id": "c1"}, {"amount": -2}},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("matching sum failed: %v %s", err, resultText(t, res))
	}
	if len(api.splitsSet) != 1 || len(api.splitsSet[0]) != 2 {
		t.Errorf("splitsSet = %v", api.splitsSet)
	}
}

func TestMCPCreateRuleReturnsDiffedID(t *testing.T) {
	api := &fakeAPI{rulesSeq: [][]*monarch.Rule{
		{{ID: "r1"}},             // before
		{{ID: "r1"}, {ID: "r2"}}, // after
	}}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_rule",
		Arguments: map[string]any{
			"merchant_contains": []string{"NETFLIX"},
			"set_category_id":   "c9",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out createRuleOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Created || out.RuleID != "r2" {
		t.Errorf("out = %+v, want diffed rule id r2", out)
	}
	if len(api.ruleCreates) != 1 || api.ruleCreates[0].ApplyToExistingTransactions {
		t.Errorf("ruleCreates = %+v — apply_to_existing must default false", api.ruleCreates)
	}
}

func TestMCPSetBudgetAmount(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "set_budget_amount",
		Arguments: map[string]any{"category_id": "c1", "amount": 600, "month": "2026-09"},
	})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	if len(api.budgetSets) != 1 || api.budgetSets[0].CategoryID != "c1" || api.budgetSets[0].Amount != 600 {
		t.Errorf("budgetSets = %+v", api.budgetSets)
	}
	if got := api.budgetSets[0].StartDate.Format("2006-01-02"); got != "2026-09-01" {
		t.Errorf("StartDate = %s", got)
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

func TestMCPNewWriteConfirmGates(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	for tool, args := range map[string]map[string]any{
		"delete_transaction": {"transaction_id": "t1"},
		"delete_tag":         {"tag_id": "g1"},
		"merge_merchants":    {"duplicate_merchant_id": "m1", "target_merchant_id": "m2"},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
			t.Errorf("%s without confirm must refuse: %s", tool, resultText(t, res))
		}
	}
	if len(api.txDeletes)+len(api.tagDeletes)+len(api.merges) != 0 {
		t.Error("no destructive op may reach the API without confirm")
	}
	// Confirmed merge reaches the API with the right ids.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "merge_merchants",
		Arguments: map[string]any{"duplicate_merchant_id": "m1", "target_merchant_id": "m2", "confirm": true},
	})
	if err != nil || res.IsError {
		t.Fatalf("confirmed merge failed: %v %s", err, resultText(t, res))
	}
	if len(api.merges) != 1 || api.merges[0] != [2]string{"m1", "m2"} {
		t.Errorf("merges = %v", api.merges)
	}
}

func TestMCPBulkUpdateTransactions(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	// Dry-run is the default: no writes.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "bulk_update_transactions",
		Arguments: map[string]any{"transaction_ids": []string{"t1", "t2"}, "needs_review": false},
	})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	if len(api.updates) != 0 {
		t.Fatal("dry-run must not write")
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out bulkUpdateOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.DryRun || out.Count != 2 || len(out.Planned) != 2 {
		t.Errorf("out = %+v", out)
	}
	// Execute.
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "bulk_update_transactions",
		Arguments: map[string]any{"transaction_ids": []string{"t1", "t2"}, "needs_review": false, "dry_run": false},
	})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	if len(api.updates) != 2 || api.updates[0].NeedsReview == nil || *api.updates[0].NeedsReview {
		t.Errorf("updates = %+v", api.updates)
	}
	// Nothing-to-change is rejected.
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "bulk_update_transactions",
		Arguments: map[string]any{"transaction_ids": []string{"t1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("bulk update with no fields must be a tool error")
	}
}

func TestMCPMergeRejectsUnknownMerchant(t *testing.T) {
	// The fake ListMerchants knows m1 and m2; a merge into an unknown
	// target must fail BEFORE the destructive call.
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "merge_merchants",
		Arguments: map[string]any{"duplicate_merchant_id": "m1", "target_merchant_id": "nope", "confirm": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("unknown target must be rejected: %s", resultText(t, res))
	}
	if len(api.merges) != 0 {
		t.Error("no merge may reach the API with an unknown target")
	}
}

func TestMCPGetCreditScore(t *testing.T) {
	cs := startMCP(t, &fakeAPI{}, false, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_credit_score", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out getCreditScoreOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Latest != 780 || len(out.Snapshots) != 1 {
		t.Errorf("out = %+v", out)
	}
}

func TestMCPUpdateTag(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	name := "renamed"
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "update_tag", Arguments: map[string]any{"tag_id": "g1", "name": name},
	})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	if len(api.tagUpdates) != 1 || api.tagUpdates[0] != "g1" {
		t.Errorf("tagUpdates = %v", api.tagUpdates)
	}
}

func TestMCPGoalMovesRequireConfirm(t *testing.T) {
	api := &fakeAPI{}
	cs := startMCP(t, api, true, nil)
	for _, tool := range []string{"contribute_to_goal", "withdraw_from_goal"} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      tool,
			Arguments: map[string]any{"goal_id": "g1", "account_id": "a1", "amount": 50.0},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
			t.Errorf("%s without confirm must refuse: %s", tool, resultText(t, res))
		}
	}
	if len(api.goalMoves) != 0 {
		t.Fatal("no money move without confirm")
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "contribute_to_goal",
		Arguments: map[string]any{"goal_id": "g1", "account_id": "a1", "amount": 50.0, "confirm": true},
	})
	if err != nil || res.IsError {
		t.Fatalf("confirmed contribute failed: %v %s", err, resultText(t, res))
	}
	if len(api.goalMoves) != 1 || api.goalMoves[0] != [3]string{"contribute", "g1", "a1"} {
		t.Errorf("goalMoves = %v", api.goalMoves)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out goalMoveOut
	json.Unmarshal(raw, &out)
	if out.CurrentBalance != 150 {
		t.Errorf("out = %+v, want balance echo 150", out)
	}
}

// scriptElicit returns an ElicitationHandler that answers each single-field
// form with the value keyed by that field name.
func scriptElicit(values map[string]string) func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	return func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		schema, _ := req.Params.RequestedSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		content := map[string]any{}
		for field := range props {
			if v, ok := values[field]; ok {
				content[field] = v
			}
		}
		return &mcp.ElicitResult{Action: "accept", Content: content}, nil
	}
}

func loginResult(t *testing.T, cs *mcp.ClientSession) loginOut {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "monarch_login", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("login tool error: %s", resultText(t, res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out loginOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMCPLoginAlreadyLoggedIn(t *testing.T) {
	cs := startMCPWith(t, &fakeAPI{}, &fakeAuth{email: "me@x.co"}, false, nil, nil)
	out := loginResult(t, cs)
	if !out.LoggedIn || out.Email != "me@x.co" {
		t.Errorf("out = %+v", out)
	}
}

func TestMCPLoginNoElicitationCapability(t *testing.T) {
	// Not logged in, client advertises no elicitation → graceful fallback.
	cs := startMCPWith(t, &fakeAPI{}, &fakeAuth{}, false, nil, nil)
	out := loginResult(t, cs)
	if out.LoggedIn || !strings.Contains(out.Detail, "terminal") {
		t.Errorf("out = %+v, want fallback-to-terminal", out)
	}
}

func TestMCPLoginDegradesGracefully(t *testing.T) {
	// The current protocol version disallows server-initiated elicitation
	// during a tool call, so even with an elicitation-capable client the
	// login tool must fall back to terminal guidance — never a hard error,
	// and never a spurious "logged in".
	auth := &fakeAuth{}
	cs := startMCPWith(t, &fakeAPI{}, auth, false, nil,
		scriptElicit(map[string]string{"email": "me@x.co", "password": "pw"}))
	out := loginResult(t, cs) // asserts no IsError
	if out.LoggedIn {
		t.Errorf("must not report logged-in when elicitation is unavailable: %+v", out)
	}
	if !strings.Contains(out.Detail, "terminal") {
		t.Errorf("detail = %q, want terminal guidance", out.Detail)
	}
	if len(auth.calls) != 0 {
		t.Errorf("no login attempt should occur when the form can't be shown: %v", auth.calls)
	}
}

func TestMCPRefreshAccounts(t *testing.T) {
	// Gated behind writes.
	ro := &fakeAPI{}
	cs := startMCP(t, ro, false, nil)
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "refresh_accounts", Arguments: map[string]any{}}); err == nil {
		t.Error("refresh_accounts must be unregistered without writes")
	}
	// With writes: triggers, returns an operation id.
	api := &fakeAPI{}
	cs = startMCP(t, api, true, nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "refresh_accounts", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	if !api.refreshed {
		t.Error("refresh not triggered")
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out refreshAccountsOut
	json.Unmarshal(raw, &out)
	if out.OperationID != "op-1" {
		t.Errorf("out = %+v", out)
	}
	// refresh_status is a read (available without writes).
	rs := startMCP(t, &fakeAPI{}, false, nil)
	res, err = rs.CallTool(context.Background(), &mcp.CallToolParams{Name: "refresh_status", Arguments: map[string]any{"operation_id": "op-1"}})
	if err != nil || res.IsError {
		t.Fatalf("%v %s", err, resultText(t, res))
	}
	raw, _ = json.Marshal(res.StructuredContent)
	var st refreshStatusOut
	json.Unmarshal(raw, &st)
	if !st.Done || st.Total != 3 {
		t.Errorf("status = %+v", st)
	}
}
