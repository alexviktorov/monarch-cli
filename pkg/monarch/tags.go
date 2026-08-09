package monarch

import "context"

type TagsService struct{ c *Client }

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const queryGetTags = `query GetHouseholdTransactionTags {
  householdTransactionTags {
    id
    name
  }
}`

func (s *TagsService) List(ctx context.Context) ([]*Tag, error) {
	var out struct {
		Tags []*Tag `json:"householdTransactionTags"`
	}
	if err := s.c.doGraphQL(ctx, "GetHouseholdTransactionTags", queryGetTags, nil, &out); err != nil {
		return nil, err
	}
	return out.Tags, nil
}
