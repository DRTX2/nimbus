package httpapi

import "testing"

func TestRoleAllows(t *testing.T) {
	tests := []struct {
		role, permission string
		want             bool
	}{
		{role: "owner", permission: "identity:write", want: true},
		{role: "operator", permission: "pipeline:write", want: true},
		{role: "operator", permission: "approval:decide", want: false},
		{role: "approver", permission: "approval:decide", want: true},
		{role: "viewer", permission: "workflow:write", want: false},
	}
	for _, test := range tests {
		if got := roleAllows(test.role, test.permission); got != test.want {
			t.Fatalf("roleAllows(%q, %q) = %v, want %v", test.role, test.permission, got, test.want)
		}
	}
}
