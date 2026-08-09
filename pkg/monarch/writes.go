package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	Message string `json:"message"`
	Code    string `json:"code"`
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
		if one == (payloadError{}) {
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
	}
	return fmt.Errorf("monarch: %s: %s", operation, strings.Join(msgs, "; "))
}

// TransactionUpdate is a partial update: nil fields are left unchanged;
// a pointer to the empty string clears notes.
type TransactionUpdate struct {
	CategoryID *string
	Notes      *string
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
	if p.CategoryID == nil && p.Notes == nil {
		return nil, errors.New("monarch: Update: nothing to change")
	}
	input := map[string]any{"id": id}
	if p.CategoryID != nil {
		input["category"] = *p.CategoryID
	}
	if p.Notes != nil {
		input["notes"] = *p.Notes
	}
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
