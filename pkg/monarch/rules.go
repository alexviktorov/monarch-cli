package monarch

import (
	"context"
	"encoding/json"
)

type RulesService struct{ c *Client }

// Rule is one auto-categorization rule. Criteria shapes vary by rule kind,
// so they are kept as raw JSON for display rather than risking a typed
// decode against an unpinned schema.
type Rule struct {
	ID                       string          `json:"id"`
	Order                    int             `json:"order"`
	MerchantCriteria         json.RawMessage `json:"merchantCriteria"`
	AmountCriteria           json.RawMessage `json:"amountCriteria"`
	CategoryIDs              []string        `json:"categoryIds"`
	AccountIDs               []string        `json:"accountIds"`
	SetCategoryAction        *CategoryRef    `json:"setCategoryAction"`
	SetMerchantAction        *MerchantRef    `json:"setMerchantAction"`
	AddTagsAction            []*Tag          `json:"addTagsAction"`
	SetHideFromReportsAction *bool           `json:"setHideFromReportsAction"`
	ReviewStatusAction       string          `json:"reviewStatusAction"`
	RecentApplicationCount   int             `json:"recentApplicationCount"`
	LastAppliedAt            string          `json:"lastAppliedAt"`
}

const queryRules = `query GetTransactionRules {
  transactionRules {
    id
    order
    merchantCriteria { operator value }
    amountCriteria { operator isExpense value }
    categoryIds
    accountIds
    setCategoryAction { id name }
    setMerchantAction { id name }
    addTagsAction { id name }
    setHideFromReportsAction
    reviewStatusAction
    recentApplicationCount
    lastAppliedAt
  }
}`

func (s *RulesService) List(ctx context.Context) ([]*Rule, error) {
	var out struct {
		TransactionRules []*Rule `json:"transactionRules"`
	}
	if err := s.c.doGraphQL(ctx, "GetTransactionRules", queryRules, nil, &out); err != nil {
		return nil, err
	}
	return out.TransactionRules, nil
}
