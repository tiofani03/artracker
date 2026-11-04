package auth

import "testing"

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		r, min Role
		want   bool
	}{
		{RoleOwner, RoleAdmin, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleViewer, RoleMember, false},
		{Role("god"), RoleViewer, false},
	}
	for _, c := range cases {
		if got := c.r.AtLeast(c.min); got != c.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", c.r, c.min, got, c.want)
		}
	}
}

func TestParseRole(t *testing.T) {
	if r, ok := ParseRole("admin"); !ok || r != RoleAdmin {
		t.Errorf("ParseRole(admin) = %v, %v", r, ok)
	}
	if _, ok := ParseRole("root"); ok {
		t.Error("ParseRole(root) should be false")
	}
}
