package monarch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mustDecode(t *testing.T, r *http.Request, into *gqlRequest) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		t.Fatalf("decode request: %v", err)
	}
}

func writesClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	c := testClient(t, handler, WithToken("test-token"), WithWritesEnabled())
	c.session.DeviceUUID = "test-device-uuid"
	return c
}

func TestUpdateTransactionGated(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("gated write must not reach the network")
	}))
	notes := "x"
	_, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{Notes: &notes})
	if !errors.Is(err, ErrWritesDisabled) {
		t.Fatalf("err = %v, want ErrWritesDisabled", err)
	}
	if _, err := c.Transactions.SetTags(context.Background(), "t1", nil); !errors.Is(err, ErrWritesDisabled) {
		t.Fatalf("SetTags err = %v, want ErrWritesDisabled", err)
	}
}

func TestUpdateTransaction(t *testing.T) {
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		if req.OperationName != "Web_TransactionDrawerUpdateTransaction" {
			t.Errorf("operation = %q", req.OperationName)
		}
		vars = req.Variables
		w.Write([]byte(`{"data":{"updateTransaction":{"transaction":
			{"id":"t1","notes":"fixed","category":{"id":"c2","name":"Dining"}},"errors":[]}}}`))
	}))
	cat, notes := "c2", "fixed"
	tx, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{CategoryID: &cat, Notes: &notes})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	if input["id"] != "t1" || input["category"] != "c2" || input["notes"] != "fixed" {
		t.Errorf("input = %v", input)
	}
	if tx.Category.Name != "Dining" || tx.Notes != "fixed" {
		t.Errorf("tx = %+v", tx)
	}
}

func TestUpdateTransactionPartial(t *testing.T) {
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		vars = req.Variables
		w.Write([]byte(`{"data":{"updateTransaction":{"transaction":{"id":"t1"},"errors":[]}}}`))
	}))
	notes := ""
	if _, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{Notes: &notes}); err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	if _, hasCategory := input["category"]; hasCategory {
		t.Error("nil CategoryID must not be sent")
	}
	if v, ok := input["notes"]; !ok || v != "" {
		t.Error("empty-string notes must be sent (clears notes)")
	}
}

func TestUpdateTransactionNothingToChange(t *testing.T) {
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no-op update must not reach the network")
	}))
	if _, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{}); err == nil {
		t.Error("expected error for empty update")
	}
}

func TestUpdateTransactionPayloadError(t *testing.T) {
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"updateTransaction":{"transaction":null,
			"errors":[{"message":"Category not found"}]}}}`))
	}))
	notes := "x"
	if _, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{Notes: &notes}); err == nil {
		t.Error("payload errors must surface as an error")
	}
}

func TestUpdateTransactionPayloadErrorObjectShape(t *testing.T) {
	// Monarch returns mutation payload errors as a single PayloadError
	// OBJECT (not an array) — the shape that actually occurs on failures.
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"updateTransaction":{"transaction":null,
			"errors":{"message":"Category not found","code":"NOT_FOUND","fieldErrors":[]}}}}`))
	}))
	notes := "x"
	_, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{Notes: &notes})
	if err == nil {
		t.Fatal("object-shaped payload error must surface as an error")
	}
	if got := err.Error(); !strings.Contains(got, "Category not found") || !strings.Contains(got, "NOT_FOUND") {
		t.Errorf("err = %q, want the server's message and code", got)
	}
}

func TestSetTags(t *testing.T) {
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		if req.OperationName != "Web_SetTransactionTags" {
			t.Errorf("operation = %q", req.OperationName)
		}
		vars = req.Variables
		w.Write([]byte(`{"data":{"setTransactionTags":{"transaction":
			{"id":"t1","tags":[{"id":"g1","name":"family"}]},"errors":[]}}}`))
	}))
	tags, err := c.Transactions.SetTags(context.Background(), "t1", []string{"g1"})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	if input["transactionId"] != "t1" {
		t.Errorf("input = %v", input)
	}
	if len(tags) != 1 || tags[0].Name != "family" {
		t.Errorf("tags = %+v", tags)
	}
}

func TestSetTagsEmptyClearsAll(t *testing.T) {
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		vars = req.Variables
		w.Write([]byte(`{"data":{"setTransactionTags":{"transaction":{"id":"t1","tags":[]},"errors":[]}}}`))
	}))
	if _, err := c.Transactions.SetTags(context.Background(), "t1", nil); err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	ids, ok := input["tagIds"].([]any)
	if !ok || len(ids) != 0 {
		t.Errorf("tagIds = %v, want empty array (clears tags), not absent", input["tagIds"])
	}
}

func TestUpdateTransactionExtendedFields(t *testing.T) {
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		vars = req.Variables
		w.Write([]byte(`{"data":{"updateTransaction":{"transaction":{"id":"t1"},"errors":[]}}}`))
	}))
	amount, merchant, hide, review := -42.5, "Costco", true, false
	d, err := time.Parse("2006-01-02", "2026-08-05")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Transactions.Update(context.Background(), "t1", TransactionUpdate{
		Amount: &amount, Date: &d, MerchantName: &merchant,
		HideFromReports: &hide, NeedsReview: &review,
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	if input["amount"] != -42.5 || input["date"] != "2026-08-05" || input["name"] != "Costco" {
		t.Errorf("input = %v", input)
	}
	if input["hideFromReports"] != true || input["needsReview"] != false {
		t.Errorf("bool fields = %v", input)
	}
	if _, has := input["category"]; has {
		t.Error("unset category must not be sent")
	}
}

func TestTagsCreate(t *testing.T) {
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		if req.OperationName != "Common_CreateTransactionTag" {
			t.Errorf("operation = %q", req.OperationName)
		}
		vars = req.Variables
		w.Write([]byte(`{"data":{"createTransactionTag":{"tag":{"id":"g9","name":"vacations"},"errors":[]}}}`))
	}))
	tag, err := c.Tags.Create(context.Background(), "vacations", "#e11d21")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	if input["name"] != "vacations" || input["color"] != "#e11d21" {
		t.Errorf("input = %v", input)
	}
	if tag.ID != "g9" {
		t.Errorf("tag = %+v", tag)
	}
}

func TestTagsCreateDefaultsColor(t *testing.T) {
	// The API requires color in CreateTransactionTagInput; an empty color
	// must be replaced with the default, never omitted.
	var vars map[string]any
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		vars = req.Variables
		w.Write([]byte(`{"data":{"createTransactionTag":{"tag":{"id":"g1","name":"x"},"errors":[]}}}`))
	}))
	if _, err := c.Tags.Create(context.Background(), "x", ""); err != nil {
		t.Fatal(err)
	}
	input, _ := vars["input"].(map[string]any)
	if input["color"] != defaultTagColor {
		t.Errorf("color = %v, want the default %s", input["color"], defaultTagColor)
	}
}

func TestTagsCreateGated(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("gated write must not reach the network")
	}))
	if _, err := c.Tags.Create(context.Background(), "x", ""); !errors.Is(err, ErrWritesDisabled) {
		t.Fatalf("err = %v, want ErrWritesDisabled", err)
	}
}

func TestWritesNeverRetry(t *testing.T) {
	var calls atomic.Int32
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	notes := "x"
	if _, err := c.Transactions.Update(context.Background(), "t1", TransactionUpdate{Notes: &notes}); err == nil {
		t.Fatal("expected error")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1 — a write must never double-fire", n)
	}
}

// ---- new write clusters (2026-08-09 expansion) ----

func writesJSON(t *testing.T, response string) (*Client, *map[string]any) {
	t.Helper()
	vars := &map[string]any{}
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		mustDecode(t, r, &req)
		*vars = req.Variables
		w.Write([]byte(response))
	}))
	return c, vars
}

func TestSetSplitsWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"updateTransactionSplit":{"errors":[],"transaction":
		{"id":"t1","amount":-100,"hasSplitTransactions":true,"splitTransactions":[
			{"id":"s1","amount":-60,"notes":"","merchant":null,"category":{"id":"c1","name":"A"}},
			{"id":"s2","amount":-40,"notes":"","merchant":null,"category":{"id":"c2","name":"B"}}
		]}}}}`)
	res, err := c.Transactions.SetSplits(context.Background(), "t1", []SplitInput{
		{Amount: -60, CategoryID: "c1"},
		{Amount: -40, CategoryID: "c2", Notes: "half"},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["transactionId"] != "t1" {
		t.Errorf("input = %v", input)
	}
	splits, _ := input["splitData"].([]any)
	if len(splits) != 2 {
		t.Fatalf("splitData = %v", input["splitData"])
	}
	first, _ := splits[0].(map[string]any)
	if _, has := first["merchantName"]; has {
		t.Error("blank merchantName must be omitted")
	}
	if _, has := first["notes"]; has {
		t.Error("blank notes must be omitted")
	}
	if res.Amount != -100 || len(res.Splits) != 2 || !res.HasSplitTransactions {
		t.Errorf("result = %+v", res)
	}
}

func TestSetSplitsClearSendsEmptyArray(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"updateTransactionSplit":{"errors":[],"transaction":
		{"id":"t1","amount":-100,"hasSplitTransactions":false,"splitTransactions":[]}}}}`)
	if _, err := c.Transactions.SetSplits(context.Background(), "t1", nil); err != nil {
		t.Fatal(err)
	}
	input, _ := (*vars)["input"].(map[string]any)
	arr, ok := input["splitData"].([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("splitData = %v, want present empty array (clears splits)", input["splitData"])
	}
}

func TestCreateTransactionWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"createTransaction":{"errors":[],"transaction":{"id":"new-tx"}}}}`)
	d, _ := time.Parse("2006-01-02", "2026-08-09")
	id, err := c.Transactions.Create(context.Background(), CreateTransactionParams{
		AccountID: "a1", Date: d, Amount: -12.345, CategoryID: "c1", MerchantName: "Test Shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "new-tx" {
		t.Errorf("id = %q", id)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["date"] != "2026-08-09" || input["accountId"] != "a1" || input["categoryId"] != "c1" {
		t.Errorf("input = %v", input)
	}
	if input["amount"] != -12.35 {
		t.Errorf("amount = %v, want rounded to 2dp (-12.35)", input["amount"])
	}
	if input["shouldUpdateBalance"] != false {
		t.Errorf("shouldUpdateBalance = %v, want always sent", input["shouldUpdateBalance"])
	}
}

func TestCreateTransactionRequiresCategory(t *testing.T) {
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not reach network without a category")
	}))
	d, _ := time.Parse("2006-01-02", "2026-08-09")
	if _, err := c.Transactions.Create(context.Background(), CreateTransactionParams{
		AccountID: "a1", Date: d, Amount: -1,
	}); err == nil {
		t.Error("expected CategoryID-required error")
	}
}

func TestBudgetSetAmountWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"updateOrCreateBudgetItem":{"budgetItem":{"id":"b1","budgetAmount":600}}}}`)
	d, _ := time.Parse("2006-01-02", "2026-08-15") // mid-month: must normalize
	item, err := c.Budgets.SetAmount(context.Background(), BudgetItemParams{
		StartDate: d, CategoryID: "c1", Amount: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.BudgetAmount != 600 {
		t.Errorf("item = %+v", item)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["startDate"] != "2026-08-01" {
		t.Errorf("startDate = %v, want normalized to first of month", input["startDate"])
	}
	if input["timeframe"] != "month" || input["categoryId"] != "c1" {
		t.Errorf("input = %v", input)
	}
	if _, has := input["applyToFuture"]; !has {
		t.Error("applyToFuture must always be sent (and spelled exactly so)")
	}
	if _, has := input["applyToFutureMonths"]; has {
		t.Error("applyToFutureMonths is the WRONG key")
	}
	if _, has := input["categoryGroupId"]; has {
		t.Error("unused categoryGroupId must be omitted")
	}
}

func TestBudgetSetAmountExactlyOneOf(t *testing.T) {
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not reach network")
	}))
	d, _ := time.Parse("2006-01-02", "2026-08-01")
	if _, err := c.Budgets.SetAmount(context.Background(), BudgetItemParams{StartDate: d, Amount: 1}); err == nil {
		t.Error("neither id set must error")
	}
	if _, err := c.Budgets.SetAmount(context.Background(), BudgetItemParams{
		StartDate: d, CategoryID: "c1", CategoryGroupID: "g1", Amount: 1,
	}); err == nil {
		t.Error("both ids set must error")
	}
}

func TestBudgetSetAmountMissingItemIsFailure(t *testing.T) {
	// This payload has NO errors field; a missing budgetItem is the only
	// failure signal.
	c, _ := writesJSON(t, `{"data":{"updateOrCreateBudgetItem":{"budgetItem":null}}}`)
	d, _ := time.Parse("2006-01-02", "2026-08-01")
	if _, err := c.Budgets.SetAmount(context.Background(), BudgetItemParams{
		StartDate: d, CategoryID: "c1", Amount: 5,
	}); err == nil {
		t.Error("missing budgetItem must be an error")
	}
}

func TestUpdateMerchantWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"updateMerchant":{"errors":[],"merchant":{"id":"m1","name":"New Name"}}}}`)
	name := "New Name"
	freq := "monthly"
	m, err := c.Merchants.Update(context.Background(), "m1", MerchantUpdate{
		Name:       &name,
		Recurrence: &RecurrenceUpdate{Frequency: &freq},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "New Name" {
		t.Errorf("merchant = %+v", m)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["merchantId"] != "m1" || input["name"] != "New Name" {
		t.Errorf("input = %v", input)
	}
	rec, _ := input["recurrence"].(map[string]any)
	if rec["frequency"] != "monthly" {
		t.Errorf("recurrence = %v", rec)
	}
	if _, has := rec["isActive"]; has {
		t.Error("unset recurrence fields must be omitted")
	}
}

func TestUpdateMerchantNothingToChange(t *testing.T) {
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not reach network")
	}))
	if _, err := c.Merchants.Update(context.Background(), "m1", MerchantUpdate{}); err == nil {
		t.Error("empty update must error")
	}
}

func TestCategoryCreateWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"createCategory":{"errors":[],"category":
		{"id":"c9","name":"Hobbies","group":{"id":"g1","name":"Lifestyle","type":"expense"}}}}}`)
	cat, err := c.Categories.Create(context.Background(), CategoryCreate{GroupID: "g1", Name: "Hobbies"})
	if err != nil {
		t.Fatal(err)
	}
	if cat.ID != "c9" || cat.Group.Type != "expense" {
		t.Errorf("category = %+v", cat)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["group"] != "g1" {
		t.Errorf("group key = %v — the key is 'group', not groupId", input)
	}
	if input["icon"] == "" || input["icon"] == nil {
		t.Error("icon must default when unset (the API requires it)")
	}
}

func TestCategoryUpdateWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"updateCategory":{"errors":[],"category":{"id":"c9","name":"Renamed"}}}}`)
	name := "Renamed"
	if _, err := c.Categories.Update(context.Background(), "c9", CategoryUpdate{Name: &name}); err != nil {
		t.Fatal(err)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["id"] != "c9" || input["name"] != "Renamed" {
		t.Errorf("input = %v", input)
	}
	if _, has := input["icon"]; has {
		t.Error("unset icon must be omitted")
	}
}

func TestCategoryDeleteWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"deleteCategory":{"errors":[],"deleted":true}}}`)
	if err := c.Categories.Delete(context.Background(), "c9", "c1"); err != nil {
		t.Fatal(err)
	}
	// Bare variables — no input wrapper.
	if (*vars)["id"] != "c9" || (*vars)["moveToCategoryId"] != "c1" {
		t.Errorf("vars = %v", *vars)
	}
	if _, has := (*vars)["input"]; has {
		t.Error("delete uses bare vars, not an input object")
	}
}

func TestCategoryDeleteNotDeleted(t *testing.T) {
	c, _ := writesJSON(t, `{"data":{"deleteCategory":{"errors":[],"deleted":false}}}`)
	if err := c.Categories.Delete(context.Background(), "c9", ""); err == nil {
		t.Error("deleted=false must be an error")
	}
}

func TestRuleInputWireShape(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"createTransactionRuleV2":{"errors":[]}}}`)
	hide := true
	err := c.Rules.Create(context.Background(), RuleInput{
		MerchantNameCriteria: []RuleCriterion{{Operator: "contains", Value: "NETFLIX"}},
		AmountCriteria:       &RuleAmountCriterion{Operator: "gt", IsExpense: true, Value: 10},
		SetCategoryID:        "c9",
		SetMerchantName:      "Netflix",
		AddTagIDs:            []string{"g1"},
		SetHideFromReports:   &hide,
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if _, has := input["applyToExistingTransactions"]; !has {
		t.Error("applyToExistingTransactions must always be sent")
	}
	if input["applyToExistingTransactions"] != false {
		t.Error("apply must default false")
	}
	// Actions are BARE values on the wire, never objects.
	if input["setCategoryAction"] != "c9" {
		t.Errorf("setCategoryAction = %v, want bare ID string", input["setCategoryAction"])
	}
	if input["setMerchantAction"] != "Netflix" {
		t.Errorf("setMerchantAction = %v, want bare NAME string", input["setMerchantAction"])
	}
	tags, _ := input["addTagsAction"].([]any)
	if len(tags) != 1 || tags[0] != "g1" {
		t.Errorf("addTagsAction = %v, want bare ID list", input["addTagsAction"])
	}
	amt, _ := input["amountCriteria"].(map[string]any)
	if v, has := amt["valueRange"]; !has || v != nil {
		t.Errorf("valueRange = %v, must be sent explicitly as null", amt)
	}
	mc, _ := input["merchantNameCriteria"].([]any)
	if len(mc) != 1 {
		t.Errorf("merchantNameCriteria = %v", input["merchantNameCriteria"])
	}
}

func TestRuleValidation(t *testing.T) {
	c := writesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not reach network")
	}))
	// No criteria.
	if err := c.Rules.Create(context.Background(), RuleInput{SetCategoryID: "c9"}); err == nil {
		t.Error("rule without criteria must error")
	}
	// No action.
	if err := c.Rules.Create(context.Background(), RuleInput{
		MerchantNameCriteria: []RuleCriterion{{Operator: "eq", Value: "X"}},
	}); err == nil {
		t.Error("rule without action must error")
	}
}

func TestRuleUpdateAddsID(t *testing.T) {
	c, vars := writesJSON(t, `{"data":{"updateTransactionRuleV2":{"errors":[]}}}`)
	err := c.Rules.Update(context.Background(), "r1", RuleInput{
		MerchantNameCriteria: []RuleCriterion{{Operator: "eq", Value: "X"}},
		SetCategoryID:        "c9",
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := (*vars)["input"].(map[string]any)
	if input["id"] != "r1" {
		t.Errorf("input = %v", input)
	}
}

func TestRuleDeleteToleratesNullDeleted(t *testing.T) {
	// deleted can be null/absent ON SUCCESS — only explicit false or an
	// errors payload is failure.
	c, vars := writesJSON(t, `{"data":{"deleteTransactionRule":{"deleted":null,"errors":[]}}}`)
	if err := c.Rules.Delete(context.Background(), "r1"); err != nil {
		t.Fatalf("null deleted must be success, got %v", err)
	}
	if (*vars)["id"] != "r1" {
		t.Errorf("vars = %v", *vars)
	}
	c2, _ := writesJSON(t, `{"data":{"deleteTransactionRule":{"deleted":false,"errors":[]}}}`)
	if err := c2.Rules.Delete(context.Background(), "r1"); err == nil {
		t.Error("explicit deleted=false must be failure")
	}
}

func TestNewWritesAllGated(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("gated writes must not reach the network")
	}))
	ctx := context.Background()
	d, _ := time.Parse("2006-01-02", "2026-08-01")
	name := "x"
	checks := map[string]error{}
	_, checks["SetSplits"] = c.Transactions.SetSplits(ctx, "t1", nil)
	_, checks["CreateTx"] = c.Transactions.Create(ctx, CreateTransactionParams{AccountID: "a", Date: d, CategoryID: "c", Amount: 1})
	_, checks["SetBudget"] = c.Budgets.SetAmount(ctx, BudgetItemParams{StartDate: d, CategoryID: "c", Amount: 1})
	_, checks["Merchant"] = c.Merchants.Update(ctx, "m1", MerchantUpdate{Name: &name})
	_, checks["CatCreate"] = c.Categories.Create(ctx, CategoryCreate{GroupID: "g", Name: "n"})
	_, checks["CatUpdate"] = c.Categories.Update(ctx, "c1", CategoryUpdate{Name: &name})
	checks["CatDelete"] = c.Categories.Delete(ctx, "c1", "")
	checks["RuleCreate"] = c.Rules.Create(ctx, RuleInput{MerchantNameCriteria: []RuleCriterion{{Operator: "eq", Value: "x"}}, SetCategoryID: "c"})
	checks["RuleUpdate"] = c.Rules.Update(ctx, "r1", RuleInput{MerchantNameCriteria: []RuleCriterion{{Operator: "eq", Value: "x"}}, SetCategoryID: "c"})
	checks["RuleDelete"] = c.Rules.Delete(ctx, "r1")
	for name, err := range checks {
		if !errors.Is(err, ErrWritesDisabled) {
			t.Errorf("%s: err = %v, want ErrWritesDisabled", name, err)
		}
	}
}
