package monarch

import "context"

// CreditScoreSnapshot is one reported credit-score point (per household
// user).
type CreditScoreSnapshot struct {
	ReportedDate string `json:"reportedDate"`
	Score        int    `json:"score"`
	UserID       string `json:"-"`
}

// Trimmed from the web app's document: only the snapshots block is
// requested (the full document also carries spinwheel onboarding state).
const queryCreditScore = `query GetCreditScoreSnapshots {
  creditScoreSnapshots {
    reportedDate
    score
    user { id }
  }
}`

// GetCreditScoreHistory returns reported credit-score snapshots.
func (c *Client) GetCreditScoreHistory(ctx context.Context) ([]*CreditScoreSnapshot, error) {
	var out struct {
		Snapshots []struct {
			ReportedDate string `json:"reportedDate"`
			Score        int    `json:"score"`
			User         *struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"creditScoreSnapshots"`
	}
	if err := c.doGraphQL(ctx, "GetCreditScoreSnapshots", queryCreditScore, nil, &out); err != nil {
		return nil, err
	}
	snaps := make([]*CreditScoreSnapshot, 0, len(out.Snapshots))
	for _, s := range out.Snapshots {
		snap := &CreditScoreSnapshot{ReportedDate: s.ReportedDate, Score: s.Score}
		if s.User != nil {
			snap.UserID = s.User.ID
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}
