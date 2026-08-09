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
