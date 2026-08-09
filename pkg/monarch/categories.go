package monarch

import "context"

type CategoriesService struct{ c *Client }

type CategoryGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Category struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Order      int            `json:"order"`
	IsDisabled bool           `json:"isDisabled"`
	Group      *CategoryGroup `json:"group"`
}

const queryGetCategories = `query GetCategories {
  categories {
    id
    name
    order
    isDisabled
    group { id name type }
  }
}`

func (s *CategoriesService) List(ctx context.Context) ([]*Category, error) {
	var out struct {
		Categories []*Category `json:"categories"`
	}
	if err := s.c.doGraphQL(ctx, "GetCategories", queryGetCategories, nil, &out); err != nil {
		return nil, err
	}
	return out.Categories, nil
}
