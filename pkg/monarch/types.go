package monarch

import (
	"fmt"
	"strings"
	"time"
)

// Date is a calendar date. Monarch returns dates in several formats
// (YYYY-MM-DD, RFC 3339, and a zoneless datetime); all unmarshal here and
// marshal back as YYYY-MM-DD, which is what the API expects in variables.
type Date struct{ time.Time }

var dateLayouts = []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"}

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		d.Time = time.Time{}
		return nil
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			d.Time = t
			return nil
		}
	}
	return fmt.Errorf("monarch: unrecognized date %q", s)
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.Format("2006-01-02") + `"`), nil
}

// Reference shapes shared across domain types.

type CategoryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MerchantRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AccountRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}
