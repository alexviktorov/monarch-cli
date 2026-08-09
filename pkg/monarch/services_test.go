package monarch

import (
	"context"
	"encoding/json"
	"net/http"
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
		{"month":"2026-07-01","accountType":"depository","sum":5000},
		{"month":"2026-07-01","accountType":"credit","sum":-800}
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
	c := gqlServer(t, "GetTransactionCategories", `{"categories":[
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
		{"category":{"id":"c1","name":"Groceries"},"monthlyAmounts":[
			{"month":"2026-08-01","plannedCashFlowAmount":600,"actualAmount":-420.5,"remainingAmount":179.5}
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
	if r.CategoryID != "c1" || r.Amount != 600 || r.Spent != 420.5 || r.Remaining != 179.5 {
		t.Errorf("row = %+v (Spent must be the negation of actualAmount)", r)
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

func TestWritesDisabledByDefault(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent while writes are disabled")
	}))
	if err := c.requireWrites(); err != ErrWritesDisabled {
		t.Errorf("requireWrites = %v, want ErrWritesDisabled", err)
	}
}
