// Package auth holds the pure auth primitives: roles, tokens, API keys.
package auth

// Role is a member's permission level inside an organization.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

var rank = map[Role]int{RoleViewer: 1, RoleMember: 2, RoleAdmin: 3, RoleOwner: 4}

// AtLeast reports whether r grants at least the permissions of min.
// Unknown roles never qualify.
func (r Role) AtLeast(min Role) bool {
	have, ok := rank[r]
	if !ok {
		return false
	}
	return have >= rank[min]
}

func ParseRole(s string) (Role, bool) {
	r := Role(s)
	_, ok := rank[r]
	return r, ok
}
