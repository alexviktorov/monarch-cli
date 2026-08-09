package monarch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// gqlServer returns a client whose /graphql handler asserts the operation
// name, records the variables, and replies with the given data payload.
func gqlServer(t *testing.T, wantOp string, data string, vars *map[string]any) *Client {
	t.Helper()
	return authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.OperationName != wantOp {
			t.Errorf("operationName = %q, want %q", req.OperationName, wantOp)
		}
		if vars != nil {
			*vars = req.Variables
		}
		w.Write([]byte(`{"data":` + data + `}`))
	}))
}

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAccountsList(t *testing.T) {
	c := gqlServer(t, "GetAccounts", `{"accounts":[
		{"id":"a1","displayName":"Checking","isHidden":false,"includeInNetWorth":true,
		 "displayBalance":1234.56,"displayLastUpdatedAt":"2026-08-01T10:00:00Z",
		 "type":{"name":"depository","display":"Cash"},"institution":{"name":"Chase"}},
		{"id":"a2","displayName":"Old","isHidden":true,"includeInNetWorth":false,
		 "displayBalance":0,"displayLastUpdatedAt":"2026-01-01","type":null,"institution":null}
	]}`, nil)
	accounts, err := c.Accounts.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len = %d", len(accounts))
	}
	a := accounts[0]
	if a.ID != "a1" || a.Type.Display != "Cash" || a.Institution.Name != "Chase" || a.DisplayBalance != 1234.56 {
		t.Errorf("account = %+v", a)
	}
	if got := a.DisplayLastUpdatedAt.Format("2006-01-02"); got != "2026-08-01" {
		t.Errorf("DisplayLastUpdatedAt = %s", got)
	}
	if accounts[1].Type != nil {
		t.Error("nil type should stay nil")
	}
}

func TestAccountsSnapshots(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetSnapshotsByAccountType", `{"snapshotsByAccountType":[
		{"month":"2026-07-01","accountType":"depository","balance":5000},
		{"month":"2026-07-01","accountType":"credit","balance":-800}
	]}`, &vars)
	snaps, err := c.Accounts.GetSnapshots(context.Background(), SnapshotParams{
		StartDate: date("2025-08-01"),
		Timeframe: "month",
	})
	if err != nil {
		t.Fatal(err)
	}
	if vars["startDate"] != "2025-08-01" || vars["timeframe"] != "month" {
		t.Errorf("vars = %v", vars)
	}
	if len(snaps) != 2 || snaps[0].Type != "depository" || snaps[0].TotalValue != 5000 || snaps[1].TotalValue != -800 {
		t.Errorf("snaps = %+v, %+v", snaps[0], snaps[1])
	}
}

func TestAccountsSnapshotsRejectsBadTimeframe(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent")
	}))
	if _, err := c.Accounts.GetSnapshots(context.Background(), SnapshotParams{Timeframe: "week"}); err == nil {
		t.Error("expected error for invalid timeframe")
	}
}

const txListFixture = `{"allTransactions":{"totalCount":120,"results":[
	{"id":"t1","date":"2026-08-01","amount":-52.5,"plaidName":"WF","notes":"",
	 "merchant":{"id":"m1","name":"Whole Foods"},"category":{"id":"c1","name":"Groceries"},
	 "account":{"id":"a1","displayName":"Checking"},"tags":[{"id":"g1","name":"family"}]},
	{"id":"t2","date":"2026-08-02","amount":2000,"plaidName":"","notes":"paycheck",
	 "merchant":{"id":"m2","name":"Employer"},"category":{"id":"c2","name":"Income"},
	 "account":{"id":"a1","displayName":"Checking"},"tags":[]}
]}}`

func TestTransactionsQuery(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetTransactionsList", txListFixture, &vars)
	list, err := c.Transactions.Query().
		Limit(50).Offset(50).
		Between(date("2026-08-01"), date("2026-08-31")).
		Search("food").
		WithAccounts("a1").WithCategories("c1").
		Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	filters, _ := vars["filters"].(map[string]any)
	if filters["startDate"] != "2026-08-01" || filters["endDate"] != "2026-08-31" || filters["search"] != "food" {
		t.Errorf("filters = %v", filters)
	}
	if vars["orderBy"] != "date" || vars["limit"] != float64(50) || vars["offset"] != float64(50) {
		t.Errorf("vars = %v", vars)
	}
	if list.TotalCount != 120 || !list.HasMore || list.NextOffset != 100 {
		t.Errorf("pagination = %+v", list)
	}
	tx := list.Transactions[0]
	if tx.ID != "t1" || tx.Amount != -52.5 || tx.Merchant.Name != "Whole Foods" || tx.Tags[0].Name != "family" {
		t.Errorf("tx = %+v", tx)
	}
}

func TestTransactionsMinMaxClientSideFilter(t *testing.T) {
	c := gqlServer(t, "GetTransactionsList", txListFixture, nil)
	list, err := c.Transactions.Query().WithMinAmount(100).Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Transactions) != 1 || list.Transactions[0].ID != "t2" {
		t.Errorf("min filter kept %+v", list.Transactions)
	}
	if list.TotalCount != 120 {
		t.Error("TotalCount must stay the server count")
	}
	c2 := gqlServer(t, "GetTransactionsList", txListFixture, nil)
	list, err = c2.Transactions.Query().WithMaxAmount(100).Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Transactions) != 1 || list.Transactions[0].ID != "t1" {
		t.Errorf("max filter kept %+v", list.Transactions)
	}
}

func TestTransactionsBareQuerySendsEmptyFilters(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetTransactionsList", txListFixture, &vars)
	if _, err := c.Transactions.Query().Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The API expects the filters variable even when empty.
	f, ok := vars["filters"].(map[string]any)
	if !ok {
		t.Fatalf("filters variable missing on a bare query: %v", vars)
	}
	if len(f) != 0 {
		t.Errorf("bare query filters = %v, want empty object", f)
	}
}

func TestTransactionsSummary(t *testing.T) {
	c := gqlServer(t, "GetTransactionsPage", `{"aggregates":[{"summary":
		{"avg":-12.3,"count":4200,"maxExpense":-900,"sumIncome":100000,"sumExpense":-80000,
		 "first":"2019-01-02","last":"2026-08-01"}}]}`, nil)
	s, err := c.Transactions.GetSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 4200 || s.SumExpense != -80000 || s.First != "2019-01-02" {
		t.Errorf("summary = %+v", s)
	}
}

func TestTransactionsSummaryEmptyAggregates(t *testing.T) {
	c := gqlServer(t, "GetTransactionsPage", `{"aggregates":[]}`, nil)
	if _, err := c.Transactions.GetSummary(context.Background()); err == nil {
		t.Error("expected error for empty aggregates")
	}
}

func TestCategoriesList(t *testing.T) {
	c := gqlServer(t, "GetCategories", `{"categories":[
		{"id":"c1","name":"Groceries","order":1,"isDisabled":false,
		 "group":{"id":"g1","name":"Food","type":"expense"}}
	]}`, nil)
	cats, err := c.Categories.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || cats[0].Group.Name != "Food" || cats[0].Group.Type != "expense" {
		t.Errorf("categories = %+v", cats[0])
	}
}

func TestBudgetsListNegatesSpent(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "Common_GetJointPlanningData", `{"budgetData":{"monthlyAmountsByCategory":[
		{"category":{"id":"c1","name":"Groceries","group":{"type":"expense"}},"monthlyAmounts":[
			{"month":"2026-08-01","plannedCashFlowAmount":600,"actualAmount":-420.5,"remainingAmount":179.5}
		]},
		{"category":{"id":"c9","name":"Paycheck","group":{"type":"income"}},"monthlyAmounts":[
			{"month":"2026-08-01","plannedCashFlowAmount":5000,"actualAmount":5000,"remainingAmount":0}
		]}
	]}}`, &vars)
	rows, err := c.Budgets.List(context.Background(), date("2026-08-01"), date("2026-08-31"))
	if err != nil {
		t.Fatal(err)
	}
	if vars["startDate"] != "2026-08-01" || vars["endDate"] != "2026-08-31" {
		t.Errorf("vars = %v", vars)
	}
	r := rows[0]
	if r.CategoryID != "c1" || r.Amount != 600 || r.Spent != 420.5 || r.Remaining != 179.5 || r.GroupType != "expense" {
		t.Errorf("row = %+v (Spent must be the negation of actualAmount)", r)
	}
	// Income rows must be identifiable so callers can keep them out of
	// spend totals (their negated actualAmount is NOT spend).
	income := rows[1]
	if income.GroupType != "income" || income.Spent != -5000 {
		t.Errorf("income row = %+v, want GroupType income", income)
	}
}

func TestCashflowGet(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "Web_GetCashFlowPage", `{
		"byCategory":[{"groupBy":{"category":{"id":"c1","name":"Rent"}},"summary":{"sum":-2000}}],
		"byMerchant":[{"groupBy":{"merchant":{"id":"m1","name":"Landlord"}},"summary":{"sum":-2000}}],
		"summary":[{"summary":{"sumIncome":8000,"sumExpense":-5000,"savings":3000,"savingsRate":0.375}}]
	}`, &vars)
	cf, err := c.Cashflow.Get(context.Background(), CashflowParams{
		StartDate: date("2026-08-01"), EndDate: date("2026-08-31"),
	})
	if err != nil {
		t.Fatal(err)
	}
	filters, _ := vars["filters"].(map[string]any)
	for _, key := range []string{"search", "categories", "accounts", "tags"} {
		if _, ok := filters[key]; !ok {
			t.Errorf("filters missing required empty key %q: %v", key, filters)
		}
	}
	if cf.Summary.Expense != 5000 {
		t.Errorf("Expense = %v, want positive 5000", cf.Summary.Expense)
	}
	if cf.ByCategory[0].Amount != -2000 {
		t.Errorf("category sum = %v, must stay raw-negative", cf.ByCategory[0].Amount)
	}
	if cf.Summary.SavingsRate != 0.375 || cf.ByMerchant[0].Merchant.Name != "Landlord" {
		t.Errorf("cashflow = %+v", cf)
	}
}

func TestRecurringFlattensStream(t *testing.T) {
	c := gqlServer(t, "Web_GetUpcomingRecurringTransactionItems", `{"recurringTransactionItems":[
		{"stream":{"frequency":"monthly","merchant":{"id":"m1","name":"Netflix"}},
		 "date":"2026-08-15","amount":-15.99,"category":{"id":"c9","name":"Streaming"}}
	]}`, nil)
	items, err := c.Recurring.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	it := items[0]
	if it.Frequency != "monthly" || it.Merchant.Name != "Netflix" || it.Amount != -15.99 {
		t.Errorf("item = %+v (stream fields must be flattened)", it)
	}
	if got := it.NextDate.Format("2006-01-02"); got != "2026-08-15" {
		t.Errorf("NextDate = %s", got)
	}
}

func TestTagsList(t *testing.T) {
	c := gqlServer(t, "GetHouseholdTransactionTags", `{"householdTransactionTags":[
		{"id":"g1","name":"family"},{"id":"g2","name":"work"}
	]}`, nil)
	tags, err := c.Tags.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[1].Name != "work" {
		t.Errorf("tags = %+v", tags)
	}
}

func TestTransactionsServerSideFilters(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetTransactionsList", txListFixture, &vars)
	_, err := c.Transactions.Query().
		WithTags("g1", "g2").
		WithHasNotes(true).WithHasAttachments(false).
		WithIsSplit(true).WithIsRecurring(false).
		WithHiddenFromReports(true).WithNeedsReview(true).
		Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	filters, _ := vars["filters"].(map[string]any)
	tags, _ := filters["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags filter = %v", filters["tags"])
	}
	for key, want := range map[string]bool{
		"hasNotes": true, "hasAttachments": false, "isSplit": true,
		"isRecurring": false, "hideFromReports": true, "needsReview": true,
	} {
		if got, ok := filters[key].(bool); !ok || got != want {
			t.Errorf("filter %s = %v, want %v", key, filters[key], want)
		}
	}
}

func TestTransactionsAll(t *testing.T) {
	var calls atomic.Int32
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		json.NewDecoder(r.Body).Decode(&req)
		offset := int(req.Variables["offset"].(float64))
		// 3 pages of 2 out of 5 total.
		txs := ""
		for i := offset; i < min(offset+2, 5); i++ {
			if txs != "" {
				txs += ","
			}
			txs += fmt.Sprintf(`{"id":"t%d","date":"2026-08-01","amount":-1}`, i)
		}
		calls.Add(1)
		fmt.Fprintf(w, `{"data":{"allTransactions":{"totalCount":5,"results":[%s]}}}`, txs)
	}))
	txs, err := c.Transactions.Query().Limit(2).All(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 5 || txs[4].ID != "t4" {
		t.Fatalf("got %d transactions", len(txs))
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3 pages", calls.Load())
	}
	// The cap truncates.
	calls.Store(0)
	txs, err = c.Transactions.Query().Limit(2).All(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 3 {
		t.Errorf("capped fetch = %d, want 3", len(txs))
	}
}

func TestTransactionsGetDetail(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetTransactionDetails", `{"getTransaction":{
		"id":"t1","amount":-100,"pending":false,"date":"2026-08-01",
		"hideFromReports":false,"plaidName":"COSTCO WHSE","notes":"bulk",
		"isRecurring":false,"needsReview":true,"hasSplitTransactions":true,"isSplitTransaction":false,
		"category":{"id":"c1","name":"Groceries"},"merchant":{"id":"m1","name":"Costco"},
		"account":{"id":"a1","displayName":"Visa"},"tags":[],
		"splitTransactions":[
			{"id":"s1","amount":-60,"notes":"","merchant":{"id":"m1","name":"Costco"},"category":{"id":"c1","name":"Groceries"}},
			{"id":"s2","amount":-40,"notes":"tires","merchant":{"id":"m1","name":"Costco"},"category":{"id":"c9","name":"Auto"}}
		]}}`, &vars)
	d, err := c.Transactions.Get(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if vars["id"] != "t1" {
		t.Errorf("vars = %v", vars)
	}
	if d.ID != "t1" || !d.NeedsReview || !d.HasSplitTransactions || d.PlaidName != "COSTCO WHSE" {
		t.Errorf("detail = %+v", d)
	}
	if len(d.Splits) != 2 || d.Splits[1].Category.Name != "Auto" {
		t.Errorf("splits = %+v", d.Splits)
	}
}

func TestHoldingsList(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "Web_GetHoldings", `{"portfolio":{"aggregateHoldings":{"edges":[
		{"node":{"id":"h1","quantity":10,"basis":1000,"totalValue":1500,
		 "security":{"id":"s1","name":"Vanguard Total","type":"etf","ticker":"VTI","currentPrice":150}}}
	]}}}`, &vars)
	holdings, err := c.Holdings.List(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	ids, _ := input["accountIds"].([]any)
	if len(ids) != 1 || ids[0] != "a1" {
		t.Errorf("input = %v", input)
	}
	if len(holdings) != 1 || holdings[0].Security.Ticker != "VTI" || holdings[0].TotalValue != 1500 {
		t.Errorf("holdings = %+v", holdings[0])
	}
}

func TestDailySnapshots(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetAggregateSnapshots", `{"aggregateSnapshots":[
		{"date":"2026-08-01","balance":100000.5},{"date":"2026-08-02","balance":100200}
	]}`, &vars)
	snaps, err := c.Accounts.GetDailySnapshots(context.Background(), date("2026-08-01"), time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	filters, _ := vars["filters"].(map[string]any)
	if filters["startDate"] != "2026-08-01" {
		t.Errorf("filters = %v", filters)
	}
	if _, hasEnd := filters["endDate"]; hasEnd {
		t.Error("zero end must omit endDate")
	}
	if len(snaps) != 2 || snaps[1].Balance != 100200 {
		t.Errorf("snaps = %+v", snaps)
	}
}

func TestCashflowGetSummary(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "Web_GetCashFlowSummary", `{"summary":[{"summary":
		{"sumIncome":8000,"sumExpense":-5000,"savings":3000,"savingsRate":0.375}}]}`, &vars)
	sum, err := c.Cashflow.GetSummary(context.Background(), CashflowParams{
		StartDate: date("2026-08-01"), EndDate: date("2026-08-31"),
	})
	if err != nil {
		t.Fatal(err)
	}
	filters, _ := vars["filters"].(map[string]any)
	for _, key := range []string{"search", "categories", "accounts", "tags"} {
		if _, ok := filters[key]; !ok {
			t.Errorf("filters missing required empty key %q", key)
		}
	}
	if sum.Expense != 5000 || sum.SavingsRate != 0.375 {
		t.Errorf("summary = %+v", sum)
	}
}

func TestRulesList(t *testing.T) {
	c := gqlServer(t, "GetTransactionRules", `{"transactionRules":[
		{"id":"r1","order":0,
		 "merchantCriteria":[{"operator":"contains","value":"NETFLIX"}],
		 "amountCriteria":null,"categoryIds":[],"accountIds":[],
		 "setCategoryAction":{"id":"c9","name":"Streaming"},
		 "setMerchantAction":null,"addTagsAction":[],
		 "setHideFromReportsAction":null,"reviewStatusAction":"",
		 "recentApplicationCount":12,"lastAppliedAt":"2026-08-01T10:00:00Z"}
	]}`, nil)
	rules, err := c.Rules.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := rules[0]
	if r.ID != "r1" || r.SetCategoryAction.Name != "Streaming" || r.RecentApplicationCount != 12 {
		t.Errorf("rule = %+v", r)
	}
	if len(r.MerchantCriteria) == 0 {
		t.Error("merchant criteria raw JSON should be retained")
	}
}

func TestGoalsList(t *testing.T) {
	c := gqlServer(t, "Common_SavingsGoals", `{"savingsGoals":[
		{"id":"g1","type":"savings","name":"House fund","status":"active","progress":0.42,
		 "currentBalance":42000,"targetDate":"2028-01-01","targetAmount":100000,
		 "plannedMonthlyContribution":1500,"netContribution":42000,
		 "forecastedCompletionDate":"2029-06-01"}
	]}`, nil)
	goals, err := c.Goals.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if goals[0].Name != "House fund" || goals[0].Progress != 0.42 || goals[0].TargetAmount != 100000 {
		t.Errorf("goal = %+v", goals[0])
	}
}

func TestInstitutionsList(t *testing.T) {
	c := gqlServer(t, "GetInstitutions", `{"credentials":[
		{"id":"cr1","updateRequired":true,"dataProvider":"PLAID",
		 "institution":{"id":"i1","name":"Chase","url":"https://chase.com"}}
	]}`, nil)
	creds, err := c.Institutions.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !creds[0].UpdateRequired || creds[0].Institution.Name != "Chase" {
		t.Errorf("credential = %+v", creds[0])
	}
}

func TestWritesDisabledByDefault(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent while writes are disabled")
	}))
	if err := c.requireWrites(); err != ErrWritesDisabled {
		t.Errorf("requireWrites = %v, want ErrWritesDisabled", err)
	}
}

func TestMerchantsList(t *testing.T) {
	var vars map[string]any
	c := gqlServer(t, "GetMerchantsSearch", `{"merchants":[
		{"id":"m1","name":"Netflix","transactionCount":12},
		{"id":"m2","name":"Netflix Inc","transactionCount":3}
	]}`, &vars)
	merchants, err := c.Merchants.List(context.Background(), "netflix", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if vars["search"] != "netflix" || vars["limit"] != float64(50) {
		t.Errorf("vars = %v", vars)
	}
	if len(merchants) != 2 || merchants[1].TransactionCount != 3 {
		t.Errorf("merchants = %+v", merchants)
	}
}
