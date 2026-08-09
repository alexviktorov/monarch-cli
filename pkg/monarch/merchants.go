package monarch

import "context"

// MerchantsService reads live here; its mutations live in writes.go per
// the mutation policy.
type MerchantsService struct{ c *Client }

type Merchant struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	TransactionCount int    `json:"transactionCount"`
}

// Shape verified live 2026-08-09 (no OSS reference carries this read).
const queryMerchants = `query GetMerchantsSearch($search: String, $limit: Int, $offset: Int) {
  merchants(search: $search, limit: $limit, offset: $offset) {
    id
    name
    transactionCount
  }
}`

// List searches merchants by name; empty search lists all (paged).
func (s *MerchantsService) List(ctx context.Context, search string, limit, offset int) ([]*Merchant, error) {
	if limit <= 0 {
		limit = 100
	}
	vars := map[string]any{"search": search, "limit": limit, "offset": offset}
	var out struct {
		Merchants []*Merchant `json:"merchants"`
	}
	if err := s.c.doGraphQL(ctx, "GetMerchantsSearch", queryMerchants, vars, &out); err != nil {
		return nil, err
	}
	return out.Merchants, nil
}
