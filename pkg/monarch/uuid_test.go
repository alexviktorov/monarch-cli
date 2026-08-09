package monarch

import (
	"regexp"
	"testing"
)

func TestNewUUIDv4(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]bool)
	for range 100 {
		u := newUUIDv4()
		if !pattern.MatchString(u) {
			t.Fatalf("newUUIDv4() = %q, not a v4 UUID", u)
		}
		if seen[u] {
			t.Fatalf("newUUIDv4() repeated %q", u)
		}
		seen[u] = true
	}
}
