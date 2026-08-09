package monarch

import "context"

type InstitutionsService struct{ c *Client }

type Institution struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Credential is one institution connection; UpdateRequired means the link
// is broken and explains stale balances.
type Credential struct {
	ID             string       `json:"id"`
	UpdateRequired bool         `json:"updateRequired"`
	DataProvider   string       `json:"dataProvider"`
	Institution    *Institution `json:"institution"`
}

const queryInstitutions = `query GetInstitutions {
  credentials {
    id
    updateRequired
    dataProvider
    institution { id name url }
  }
}`

func (s *InstitutionsService) List(ctx context.Context) ([]*Credential, error) {
	var out struct {
		Credentials []*Credential `json:"credentials"`
	}
	if err := s.c.doGraphQL(ctx, "GetInstitutions", queryInstitutions, nil, &out); err != nil {
		return nil, err
	}
	return out.Credentials, nil
}
