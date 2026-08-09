package monarch

import (
	"regexp"
	"testing"
)

func TestDeviceUUIDFromToken(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a1 := deviceUUIDFromToken("token-a")
	a2 := deviceUUIDFromToken("token-a")
	b := deviceUUIDFromToken("token-b")
	if a1 != a2 {
		t.Errorf("same token must derive the same device-uuid: %s vs %s", a1, a2)
	}
	if a1 == b {
		t.Error("different tokens must derive different device-uuids")
	}
	for _, u := range []string{a1, b} {
		if !pattern.MatchString(u) {
			t.Errorf("derived uuid %q is not v4-shaped", u)
		}
	}
}

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
