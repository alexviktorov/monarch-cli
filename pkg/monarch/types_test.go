package monarch

import (
	"encoding/json"
	"testing"
)

func TestDateUnmarshalFormats(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"2026-07-04"`, "2026-07-04"},
		{`"2026-07-04T12:34:56Z"`, "2026-07-04"},
		{`"2026-07-04T12:34:56"`, "2026-07-04"},
		{`null`, "zero"},
		{`""`, "zero"},
	}
	for _, c := range cases {
		var d Date
		if err := json.Unmarshal([]byte(c.in), &d); err != nil {
			t.Fatalf("Unmarshal(%s): %v", c.in, err)
		}
		if c.want == "zero" {
			if !d.IsZero() {
				t.Errorf("Unmarshal(%s): want zero date, got %v", c.in, d)
			}
			continue
		}
		if got := d.Format("2006-01-02"); got != c.want {
			t.Errorf("Unmarshal(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestDateUnmarshalRejectsGarbage(t *testing.T) {
	var d Date
	if err := json.Unmarshal([]byte(`"07/04/2026"`), &d); err == nil {
		t.Error("expected error for unrecognized date format")
	}
}

func TestDateMarshal(t *testing.T) {
	var d Date
	if err := json.Unmarshal([]byte(`"2026-07-04T12:34:56Z"`), &d); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"2026-07-04"` {
		t.Errorf("Marshal = %s, want \"2026-07-04\"", b)
	}
	var zero Date
	b, err = json.Marshal(zero)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "null" {
		t.Errorf("Marshal(zero) = %s, want null", b)
	}
}
