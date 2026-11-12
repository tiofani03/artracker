package issues

import "testing"

func TestIsFatal(t *testing.T) {
	cases := []struct {
		name, typ, title string
		want             bool
	}{
		{"TypeError type", "TypeError", "x", true},
		{"ReferenceError type", "ReferenceError", "x", true},
		{"plain NPE", "NullPointerException", "plain title", false},
		{"uncaught title", "Error", "Uncaught exception in handler", true},
		{"unhandled title", "Error", "Unhandled rejection", true},
		{"panic type", "panic", "x", true},
		{"fatal type", "FatalException", "x", true},
		{"crash title", "Error", "App crashed", true},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsFatal(c.typ, c.title); got != c.want {
				t.Fatalf("IsFatal(%q,%q)=%v want %v", c.typ, c.title, got, c.want)
			}
		})
	}
}
