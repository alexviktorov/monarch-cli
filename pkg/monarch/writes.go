package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// This file is the only home for mutations. Every mutation must:
//  1. call c.requireWrites() before doing anything,
//  2. go through doGraphQLNoRetry so an ambiguous network failure can
//     never double-fire a write against live financial data,
//  3. request only the fields needed to confirm the change.
//
// The gate is armed exclusively by WithWritesEnabled at construction time;
// there is deliberately no way to toggle it on an existing client.
//
// Mutation shapes follow the behavior observed from the Monarch web app
// (same input field names the hammem/monarchmoney reference client sends);
// re-verify against app.monarch.com devtools if a write starts failing.

func (c *Client) requireWrites() error {
	if !c.writesOK {
		return ErrWritesDisabled
	}
	return nil
}

type payloadError struct {
	Message     string `json:"message"`
	Code        string `json:"code"`
	FieldErrors []struct {
		Field    string   `json:"field"`
		Messages []string `json:"messages"`
	} `json:"fieldErrors"`
}

// payloadErrors tolerates both shapes Monarch uses for mutation payload
// errors: an array on some payloads, a single PayloadError object on others
// (the object form is what updateTransaction actually returns on failure).
type payloadErrors []payloadError

func (p *payloadErrors) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*p = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		var arr []payloadError
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return err
		}
		*p = arr
	case '{':
		var one payloadError
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return err
		}
		if one.Message == "" && one.Code == "" && len(one.FieldErrors) == 0 {
			*p = nil
		} else {
			*p = payloadErrors{one}
		}
	default:
		return fmt.Errorf("monarch: unexpected payload errors shape %q", trimmed[0])
	}
	return nil
}

func payloadErrs(operation string, errs payloadErrors) error {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Message
		if e.Code != "" {
			msgs[i] += " (" + e.Code + ")"
		}
		for _, fe := range e.FieldErrors {
			msgs[i] += fmt.Sprintf(" [%s: %s]", fe.Field, strings.Join(fe.Messages, ", "))
		}
	}
	return fmt.Errorf("monarch: %s: %s", operation, strings.Join(msgs, "; "))
}

// TransactionUpdate is a partial update: nil fields are left unchanged;
// a pointer to the empty string clears notes. Input key names follow the
// web app's UpdateTransactionMutationInput.
type TransactionUpdate struct {
	CategoryID      *string
	Notes           *string
	Amount          *float64
	Date            *time.Time
	MerchantName    *string // input key "name"; Monarch ignores empty strings
	HideFromReports *bool
	NeedsReview     *bool
}

func (p TransactionUpdate) empty() bool {
	return p.CategoryID == nil && p.Notes == nil && p.Amount == nil &&
		p.Date == nil && p.MerchantName == nil && p.HideFromReports == nil && p.NeedsReview == nil
}

func (p TransactionUpdate) input(id string) map[string]any {
	input := map[string]any{"id": id}
	if p.CategoryID != nil {
		input["category"] = *p.CategoryID
	}
	if p.Notes != nil {
		input["notes"] = *p.Notes
	}
	if p.Amount != nil {
		input["amount"] = *p.Amount
	}
	if p.Date != nil {
		input["date"] = p.Date.Format("2006-01-02")
	}
	if p.MerchantName != nil {
		input["name"] = *p.MerchantName
	}
	if p.HideFromReports != nil {
		input["hideFromReports"] = *p.HideFromReports
	}
	if p.NeedsReview != nil {
		input["needsReview"] = *p.NeedsReview
	}
	return input
}

const mutationUpdateTransaction = `mutation Web_TransactionDrawerUpdateTransaction($input: UpdateTransactionMutationInput!) {
  updateTransaction(input: $input) {
    transaction {
      id
      notes
      category { id name }
    }
    errors { message }
  }
}`

// Update applies a partial update to one transaction. Requires
// WithWritesEnabled.
func (s *TransactionsService) Update(ctx context.Context, id string, p TransactionUpdate) (*Transaction, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("monarch: Update: transaction id required")
	}
	if p.empty() {
		return nil, errors.New("monarch: Update: nothing to change")
	}
	input := p.input(id)
	var out struct {
		UpdateTransaction struct {
			Transaction *Transaction  `json:"transaction"`
			Errors      payloadErrors `json:"errors"`
		} `json:"updateTransaction"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Web_TransactionDrawerUpdateTransaction", mutationUpdateTransaction,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.UpdateTransaction.Errors) > 0 {
		return nil, payloadErrs("UpdateTransaction", out.UpdateTransaction.Errors)
	}
	return out.UpdateTransaction.Transaction, nil
}

const mutationSetTransactionTags = `mutation Web_SetTransactionTags($input: SetTransactionTagsInput!) {
  setTransactionTags(input: $input) {
    transaction {
      id
      tags { id name }
    }
    errors { message }
  }
}`

// SetTags replaces the full tag set of one transaction (an empty slice
// clears all tags). Requires WithWritesEnabled.
func (s *TransactionsService) SetTags(ctx context.Context, id string, tagIDs []string) ([]*Tag, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("monarch: SetTags: transaction id required")
	}
	if tagIDs == nil {
		tagIDs = []string{}
	}
	input := map[string]any{
		"transactionId": id,
		"tagIds":        tagIDs,
	}
	var out struct {
		SetTransactionTags struct {
			Transaction *Transaction  `json:"transaction"`
			Errors      payloadErrors `json:"errors"`
		} `json:"setTransactionTags"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Web_SetTransactionTags", mutationSetTransactionTags,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.SetTransactionTags.Errors) > 0 {
		return nil, payloadErrs("SetTransactionTags", out.SetTransactionTags.Errors)
	}
	if out.SetTransactionTags.Transaction == nil {
		return nil, nil
	}
	return out.SetTransactionTags.Transaction.Tags, nil
}

const mutationCreateTag = `mutation Common_CreateTransactionTag($input: CreateTransactionTagInput!) {
  createTransactionTag(input: $input) {
    tag { id name }
    errors { message }
  }
}`

// defaultTagColor is used when the caller doesn't pick one: the API
// REQUIRES color in CreateTransactionTagInput (verified live 2026-08-09 —
// omitting it is a 400).
const defaultTagColor = "#3b82f6"

// Create creates a transaction tag (color e.g. "#e11d21"; a default is
// supplied when empty). Requires WithWritesEnabled.
func (s *TagsService) Create(ctx context.Context, name, color string) (*Tag, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, errors.New("monarch: CreateTag: name required")
	}
	if color == "" {
		color = defaultTagColor
	}
	input := map[string]any{"name": name, "color": color}
	var out struct {
		CreateTransactionTag struct {
			Tag    *Tag          `json:"tag"`
			Errors payloadErrors `json:"errors"`
		} `json:"createTransactionTag"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_CreateTransactionTag", mutationCreateTag,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.CreateTransactionTag.Errors) > 0 {
		return nil, payloadErrs("CreateTransactionTag", out.CreateTransactionTag.Errors)
	}
	if out.CreateTransactionTag.Tag == nil {
		return nil, errors.New("monarch: CreateTransactionTag: no tag in response")
	}
	return out.CreateTransactionTag.Tag, nil
}

// ---- transaction splits ----

// SplitInput is one child in a full-replacement split set. Amounts are
// signed (Monarch convention) and must sum to the parent's amount —
// enforced server-side; fetch the parent via Get to pre-validate.
type SplitInput struct {
	Amount       float64
	CategoryID   string
	MerchantName string
	Notes        string
}

type SplitResult struct {
	TransactionID        string
	Amount               float64
	HasSplitTransactions bool
	Splits               []*TransactionSplit
}

const mutationSetSplits = `mutation Common_SplitTransactionMutation($input: UpdateTransactionSplitMutationInput!) {
  updateTransactionSplit(input: $input) {
    errors { message code fieldErrors { field messages } }
    transaction {
      id
      amount
      hasSplitTransactions
      splitTransactions {
        id
        amount
        notes
        merchant { id name }
        category { id name }
      }
    }
  }
}`

// SetSplits REPLACES the full split set of a transaction; an empty slice
// clears all splits and restores the original transaction. Requires
// WithWritesEnabled.
func (s *TransactionsService) SetSplits(ctx context.Context, txID string, splits []SplitInput) (*SplitResult, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if txID == "" {
		return nil, errors.New("monarch: SetSplits: transaction id required")
	}
	splitData := make([]map[string]any, 0, len(splits))
	for _, sp := range splits {
		m := map[string]any{"amount": sp.Amount}
		// Blank optionals are omitted: an empty-string categoryId is a
		// rejection risk.
		if sp.CategoryID != "" {
			m["categoryId"] = sp.CategoryID
		}
		if sp.MerchantName != "" {
			m["merchantName"] = sp.MerchantName
		}
		if sp.Notes != "" {
			m["notes"] = sp.Notes
		}
		splitData = append(splitData, m)
	}
	input := map[string]any{"transactionId": txID, "splitData": splitData}
	var out struct {
		UpdateTransactionSplit struct {
			Errors      payloadErrors `json:"errors"`
			Transaction *struct {
				ID                   string              `json:"id"`
				Amount               float64             `json:"amount"`
				HasSplitTransactions bool                `json:"hasSplitTransactions"`
				Splits               []*TransactionSplit `json:"splitTransactions"`
			} `json:"transaction"`
		} `json:"updateTransactionSplit"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_SplitTransactionMutation", mutationSetSplits,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.UpdateTransactionSplit.Errors) > 0 {
		return nil, payloadErrs("SplitTransaction", out.UpdateTransactionSplit.Errors)
	}
	tx := out.UpdateTransactionSplit.Transaction
	if tx == nil {
		return nil, errors.New("monarch: SplitTransaction: no transaction in response")
	}
	return &SplitResult{
		TransactionID:        tx.ID,
		Amount:               tx.Amount,
		HasSplitTransactions: tx.HasSplitTransactions,
		Splits:               tx.Splits,
	}, nil
}

// ---- create transaction ----

type CreateTransactionParams struct {
	AccountID           string
	Date                time.Time
	Amount              float64 // signed; rounded to 2dp on the wire
	CategoryID          string  // required by the API (see Categories.List)
	MerchantName        string
	Notes               string
	ShouldUpdateBalance bool
}

const mutationCreateTransaction = `mutation Common_CreateTransactionMutation($input: CreateTransactionMutationInput!) {
  createTransaction(input: $input) {
    errors { message code fieldErrors { field messages } }
    transaction { id }
  }
}`

// Create adds a transaction (intended for manual accounts) and returns the
// new transaction id. Requires WithWritesEnabled.
func (s *TransactionsService) Create(ctx context.Context, p CreateTransactionParams) (string, error) {
	if err := s.c.requireWrites(); err != nil {
		return "", err
	}
	if p.AccountID == "" || p.Date.IsZero() {
		return "", errors.New("monarch: Create: AccountID and Date are required")
	}
	if p.CategoryID == "" {
		return "", errors.New("monarch: Create: CategoryID is required by the Monarch API (use Categories.List, e.g. the Uncategorized category)")
	}
	input := map[string]any{
		"date":                p.Date.Format("2006-01-02"),
		"accountId":           p.AccountID,
		"amount":              math.Round(p.Amount*100) / 100,
		"categoryId":          p.CategoryID,
		"shouldUpdateBalance": p.ShouldUpdateBalance,
	}
	if p.MerchantName != "" {
		input["merchantName"] = p.MerchantName
	}
	if p.Notes != "" {
		input["notes"] = p.Notes
	}
	var out struct {
		CreateTransaction struct {
			Errors      payloadErrors `json:"errors"`
			Transaction *struct {
				ID string `json:"id"`
			} `json:"transaction"`
		} `json:"createTransaction"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_CreateTransactionMutation", mutationCreateTransaction,
		map[string]any{"input": input}, &out)
	if err != nil {
		return "", err
	}
	if len(out.CreateTransaction.Errors) > 0 {
		return "", payloadErrs("CreateTransaction", out.CreateTransaction.Errors)
	}
	if out.CreateTransaction.Transaction == nil {
		return "", errors.New("monarch: CreateTransaction: no transaction in response")
	}
	return out.CreateTransaction.Transaction.ID, nil
}

// ---- budget set ----

// BudgetItemParams sets the planned amount for one category XOR one
// category group for the month containing StartDate. Amount 0 clears the
// budget.
type BudgetItemParams struct {
	StartDate       time.Time // normalized to the first of its month
	CategoryID      string
	CategoryGroupID string
	Amount          float64
	ApplyToFuture   bool
}

type BudgetItem struct {
	ID           string  `json:"id"`
	BudgetAmount float64 `json:"budgetAmount"`
}

// The updateOrCreateBudgetItem payload carries NO errors field — failures
// surface as top-level GraphQL errors, and a missing budgetItem is the
// remaining failure signal.
const mutationSetBudget = `mutation Common_UpdateBudgetItem($input: UpdateOrCreateBudgetItemMutationInput!) {
  updateOrCreateBudgetItem(input: $input) {
    budgetItem { id budgetAmount }
  }
}`

// SetAmount sets or clears (amount 0) a monthly budget. Requires
// WithWritesEnabled.
func (s *BudgetsService) SetAmount(ctx context.Context, p BudgetItemParams) (*BudgetItem, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if (p.CategoryID == "") == (p.CategoryGroupID == "") {
		return nil, errors.New("monarch: SetAmount: exactly one of CategoryID or CategoryGroupID is required")
	}
	if p.StartDate.IsZero() {
		return nil, errors.New("monarch: SetAmount: StartDate required")
	}
	firstOfMonth := time.Date(p.StartDate.Year(), p.StartDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	// The key is applyToFuture — the applyToFutureMonths spelling appears
	// in no working reference.
	input := map[string]any{
		"startDate":     firstOfMonth.Format("2006-01-02"),
		"timeframe":     "month",
		"amount":        p.Amount,
		"applyToFuture": p.ApplyToFuture,
	}
	if p.CategoryID != "" {
		input["categoryId"] = p.CategoryID
	} else {
		input["categoryGroupId"] = p.CategoryGroupID
	}
	var out struct {
		UpdateOrCreateBudgetItem struct {
			BudgetItem *BudgetItem `json:"budgetItem"`
		} `json:"updateOrCreateBudgetItem"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_UpdateBudgetItem", mutationSetBudget,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateOrCreateBudgetItem.BudgetItem == nil {
		return nil, errors.New("monarch: Common_UpdateBudgetItem: no budgetItem in response")
	}
	return out.UpdateOrCreateBudgetItem.BudgetItem, nil
}

// ---- merchants ----

// MerchantsService currently exposes mutations only (the merchant read
// surface lives inside transactions/cashflow responses).
type MerchantsService struct{ c *Client }

// RecurrenceUpdate configures a merchant's recurring stream — the only
// known way to modify recurring frequency/amount/base date/active status.
type RecurrenceUpdate struct {
	IsRecurring *bool
	Frequency   *string // weekly|biweekly|twice_a_month|monthly|quarterly|semiannually|annually
	BaseDate    *time.Time
	Amount      *float64 // signed: negative = expense
	IsActive    *bool
}

type MerchantUpdate struct {
	Name       *string
	Recurrence *RecurrenceUpdate
}

type MerchantInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const mutationUpdateMerchant = `mutation Common_UpdateMerchant($input: UpdateMerchantInput!) {
  updateMerchant(input: $input) {
    merchant { id name }
    errors { message code fieldErrors { field messages } }
  }
}`

// Update renames a merchant and/or reconfigures its recurring stream.
// Requires WithWritesEnabled.
func (s *MerchantsService) Update(ctx context.Context, merchantID string, p MerchantUpdate) (*MerchantInfo, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if merchantID == "" {
		return nil, errors.New("monarch: UpdateMerchant: merchant id required")
	}
	input := map[string]any{"merchantId": merchantID}
	if p.Name != nil {
		input["name"] = *p.Name
	}
	if p.Recurrence != nil {
		rec := map[string]any{}
		if p.Recurrence.IsRecurring != nil {
			rec["isRecurring"] = *p.Recurrence.IsRecurring
		}
		if p.Recurrence.Frequency != nil {
			rec["frequency"] = *p.Recurrence.Frequency
		}
		if p.Recurrence.BaseDate != nil {
			rec["baseDate"] = p.Recurrence.BaseDate.Format("2006-01-02")
		}
		if p.Recurrence.Amount != nil {
			rec["amount"] = *p.Recurrence.Amount
		}
		if p.Recurrence.IsActive != nil {
			rec["isActive"] = *p.Recurrence.IsActive
		}
		if len(rec) > 0 {
			input["recurrence"] = rec
		}
	}
	if len(input) == 1 {
		return nil, errors.New("monarch: UpdateMerchant: nothing to change")
	}
	var out struct {
		UpdateMerchant struct {
			Merchant *MerchantInfo `json:"merchant"`
			Errors   payloadErrors `json:"errors"`
		} `json:"updateMerchant"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_UpdateMerchant", mutationUpdateMerchant,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.UpdateMerchant.Errors) > 0 {
		return nil, payloadErrs("UpdateMerchant", out.UpdateMerchant.Errors)
	}
	return out.UpdateMerchant.Merchant, nil
}

// ---- categories CRUD ----

const defaultCategoryIcon = "❓" // ❓ — the default every reference uses

type CategoryCreate struct {
	GroupID string // wire key "group"
	Name    string
	Icon    string // defaults to defaultCategoryIcon
}

const mutationCreateCategory = `mutation Web_CreateCategory($input: CreateCategoryInput!) {
  createCategory(input: $input) {
    errors { message code fieldErrors { field messages } }
    category { id name group { id name type } }
  }
}`

// Create adds a transaction category to a group. Requires WithWritesEnabled.
func (s *CategoriesService) Create(ctx context.Context, p CategoryCreate) (*Category, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if p.GroupID == "" || p.Name == "" {
		return nil, errors.New("monarch: CreateCategory: GroupID and Name are required")
	}
	icon := p.Icon
	if icon == "" {
		icon = defaultCategoryIcon
	}
	input := map[string]any{"group": p.GroupID, "name": p.Name, "icon": icon}
	var out struct {
		CreateCategory struct {
			Errors   payloadErrors `json:"errors"`
			Category *Category     `json:"category"`
		} `json:"createCategory"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Web_CreateCategory", mutationCreateCategory,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.CreateCategory.Errors) > 0 {
		return nil, payloadErrs("CreateCategory", out.CreateCategory.Errors)
	}
	if out.CreateCategory.Category == nil {
		return nil, errors.New("monarch: CreateCategory: no category in response")
	}
	return out.CreateCategory.Category, nil
}

type CategoryUpdate struct {
	Name *string
	Icon *string
}

const mutationUpdateCategory = `mutation Web_UpdateCategory($input: UpdateCategoryInput!) {
  updateCategory(input: $input) {
    errors { message code }
    category { id name }
  }
}`

// Update renames/re-icons a category. Requires WithWritesEnabled.
func (s *CategoriesService) Update(ctx context.Context, id string, p CategoryUpdate) (*Category, error) {
	if err := s.c.requireWrites(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("monarch: UpdateCategory: category id required")
	}
	if p.Name == nil && p.Icon == nil {
		return nil, errors.New("monarch: UpdateCategory: nothing to change")
	}
	input := map[string]any{"id": id}
	if p.Name != nil {
		input["name"] = *p.Name
	}
	if p.Icon != nil {
		input["icon"] = *p.Icon
	}
	var out struct {
		UpdateCategory struct {
			Errors   payloadErrors `json:"errors"`
			Category *Category     `json:"category"`
		} `json:"updateCategory"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Web_UpdateCategory", mutationUpdateCategory,
		map[string]any{"input": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.UpdateCategory.Errors) > 0 {
		return nil, payloadErrs("UpdateCategory", out.UpdateCategory.Errors)
	}
	return out.UpdateCategory.Category, nil
}

// Bare variables, not an input object — the one mutation shaped this way.
const mutationDeleteCategory = `mutation Web_DeleteCategory($id: UUID!, $moveToCategoryId: UUID) {
  deleteCategory(id: $id, moveToCategoryId: $moveToCategoryId) {
    errors { message code fieldErrors { field messages } }
    deleted
  }
}`

// Delete removes a category; moveToCategoryID (recommended) reassigns its
// transactions instead of orphaning them. Requires WithWritesEnabled.
func (s *CategoriesService) Delete(ctx context.Context, id, moveToCategoryID string) error {
	if err := s.c.requireWrites(); err != nil {
		return err
	}
	if id == "" {
		return errors.New("monarch: DeleteCategory: category id required")
	}
	vars := map[string]any{"id": id}
	if moveToCategoryID != "" {
		vars["moveToCategoryId"] = moveToCategoryID
	}
	var out struct {
		DeleteCategory struct {
			Errors  payloadErrors `json:"errors"`
			Deleted bool          `json:"deleted"`
		} `json:"deleteCategory"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Web_DeleteCategory", mutationDeleteCategory, vars, &out)
	if err != nil {
		return err
	}
	if len(out.DeleteCategory.Errors) > 0 {
		return payloadErrs("DeleteCategory", out.DeleteCategory.Errors)
	}
	if !out.DeleteCategory.Deleted {
		return errors.New("monarch: DeleteCategory: category was not deleted")
	}
	return nil
}

// ---- rules CRUD ----

type RuleCriterion struct {
	Operator string // "eq" | "contains"
	Value    string
}

type RuleAmountCriterion struct {
	Operator  string // "gt" | "lt" | "eq"
	IsExpense bool
	Value     float64
}

// RuleInput builds Create/UpdateTransactionRuleInput. On the WIRE the
// actions are bare values (category ID string, merchant NAME string, tag ID
// list) even though rule READS return them as objects — never round-trip a
// read struct into this input.
type RuleInput struct {
	ApplyToExistingTransactions bool // back-applies to history — the blast radius
	MerchantNameCriteria        []RuleCriterion
	AmountCriteria              *RuleAmountCriterion
	AccountIDs                  []string
	SetCategoryID               string
	SetMerchantName             string
	AddTagIDs                   []string
	SetHideFromReports          *bool
	ReviewStatusAction          string // "needs_review" or ""
}

func (r RuleInput) validate() error {
	if len(r.MerchantNameCriteria) == 0 && r.AmountCriteria == nil && len(r.AccountIDs) == 0 {
		return errors.New("monarch: rule needs at least one criterion (merchant, amount, or accounts)")
	}
	if r.SetCategoryID == "" && r.SetMerchantName == "" && len(r.AddTagIDs) == 0 &&
		r.SetHideFromReports == nil && r.ReviewStatusAction == "" {
		return errors.New("monarch: rule needs at least one action")
	}
	return nil
}

func (r RuleInput) input() map[string]any {
	input := map[string]any{
		// Always sent, even when false — matches every working reference.
		"applyToExistingTransactions": r.ApplyToExistingTransactions,
	}
	if len(r.MerchantNameCriteria) > 0 {
		list := make([]map[string]any, 0, len(r.MerchantNameCriteria))
		for _, c := range r.MerchantNameCriteria {
			list = append(list, map[string]any{"operator": c.Operator, "value": c.Value})
		}
		input["merchantNameCriteria"] = list
	}
	if r.AmountCriteria != nil {
		input["amountCriteria"] = map[string]any{
			"operator":   r.AmountCriteria.Operator,
			"isExpense":  r.AmountCriteria.IsExpense,
			"value":      r.AmountCriteria.Value,
			"valueRange": nil, // sent explicitly as null by every reference
		}
	}
	if len(r.AccountIDs) > 0 {
		input["accountIds"] = r.AccountIDs
	}
	if r.SetCategoryID != "" {
		input["setCategoryAction"] = r.SetCategoryID
	}
	if r.SetMerchantName != "" {
		input["setMerchantAction"] = r.SetMerchantName
	}
	if len(r.AddTagIDs) > 0 {
		input["addTagsAction"] = r.AddTagIDs
	}
	if r.SetHideFromReports != nil {
		input["setHideFromReportsAction"] = *r.SetHideFromReports
	}
	if r.ReviewStatusAction != "" {
		input["reviewStatusAction"] = r.ReviewStatusAction
	}
	return input
}

// Create/update responses are ERRORS-ONLY: no entity, no id. Re-run
// Rules.List afterwards to find the new rule.
const mutationCreateRule = `mutation Common_CreateTransactionRuleMutationV2($input: CreateTransactionRuleInput!) {
  createTransactionRuleV2(input: $input) {
    errors { message code fieldErrors { field messages } }
  }
}`

const mutationUpdateRule = `mutation Common_UpdateTransactionRuleMutationV2($input: UpdateTransactionRuleInput!) {
  updateTransactionRuleV2(input: $input) {
    errors { message code fieldErrors { field messages } }
  }
}`

const mutationDeleteRule = `mutation Common_DeleteTransactionRule($id: ID!) {
  deleteTransactionRule(id: $id) {
    deleted
    errors { message code fieldErrors { field messages } }
  }
}`

// Create adds an auto-categorization rule. Requires WithWritesEnabled.
func (s *RulesService) Create(ctx context.Context, r RuleInput) error {
	if err := s.c.requireWrites(); err != nil {
		return err
	}
	if err := r.validate(); err != nil {
		return err
	}
	var out struct {
		CreateTransactionRuleV2 struct {
			Errors payloadErrors `json:"errors"`
		} `json:"createTransactionRuleV2"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_CreateTransactionRuleMutationV2", mutationCreateRule,
		map[string]any{"input": r.input()}, &out)
	if err != nil {
		return err
	}
	if len(out.CreateTransactionRuleV2.Errors) > 0 {
		return payloadErrs("CreateTransactionRule", out.CreateTransactionRuleV2.Errors)
	}
	return nil
}

// Update replaces a rule's definition. Requires WithWritesEnabled.
func (s *RulesService) Update(ctx context.Context, id string, r RuleInput) error {
	if err := s.c.requireWrites(); err != nil {
		return err
	}
	if id == "" {
		return errors.New("monarch: UpdateRule: rule id required")
	}
	if err := r.validate(); err != nil {
		return err
	}
	input := r.input()
	input["id"] = id
	var out struct {
		UpdateTransactionRuleV2 struct {
			Errors payloadErrors `json:"errors"`
		} `json:"updateTransactionRuleV2"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_UpdateTransactionRuleMutationV2", mutationUpdateRule,
		map[string]any{"input": input}, &out)
	if err != nil {
		return err
	}
	if len(out.UpdateTransactionRuleV2.Errors) > 0 {
		return payloadErrs("UpdateTransactionRule", out.UpdateTransactionRuleV2.Errors)
	}
	return nil
}

// Delete removes a rule. The API may return deleted as null/absent on
// SUCCESS, so only an explicit false (or an errors payload) is failure.
// Requires WithWritesEnabled.
func (s *RulesService) Delete(ctx context.Context, id string) error {
	if err := s.c.requireWrites(); err != nil {
		return err
	}
	if id == "" {
		return errors.New("monarch: DeleteRule: rule id required")
	}
	var out struct {
		DeleteTransactionRule struct {
			Deleted *bool         `json:"deleted"`
			Errors  payloadErrors `json:"errors"`
		} `json:"deleteTransactionRule"`
	}
	err := s.c.doGraphQLNoRetry(ctx, "Common_DeleteTransactionRule", mutationDeleteRule,
		map[string]any{"id": id}, &out)
	if err != nil {
		return err
	}
	if len(out.DeleteTransactionRule.Errors) > 0 {
		return payloadErrs("DeleteTransactionRule", out.DeleteTransactionRule.Errors)
	}
	if out.DeleteTransactionRule.Deleted != nil && !*out.DeleteTransactionRule.Deleted {
		return errors.New("monarch: DeleteTransactionRule: rule was not deleted")
	}
	return nil
}
