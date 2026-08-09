package monarch

// This file is the only home for mutations. Every mutation must:
//  1. call c.requireWrites() before doing anything,
//  2. go through doGraphQLNoRetry so an ambiguous network failure can
//     never double-fire a write against live financial data,
//  3. request only the fields needed to confirm the change.
//
// The gate is armed exclusively by WithWritesEnabled at construction time;
// there is deliberately no way to toggle it on an existing client.

func (c *Client) requireWrites() error {
	if !c.writesOK {
		return ErrWritesDisabled
	}
	return nil
}
