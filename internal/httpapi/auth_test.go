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

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
		ok    bool
	}{
		{name: "standard", value: "Bearer token", want: "token", ok: true},
		{name: "case insensitive scheme", value: "bearer token", want: "token", ok: true},
		{name: "surrounding whitespace", value: "  Bearer   token  ", want: "token", ok: true},
		{name: "raw token", value: "token"},
		{name: "missing token", value: "Bearer"},
		{name: "extra field", value: "Bearer token extra"},
		{name: "wrong scheme", value: "Basic token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := bearerToken(test.value)
			if got != test.want || ok != test.ok {
				t.Fatalf("bearerToken(%q) = (%q, %v), want (%q, %v)", test.value, got, ok, test.want, test.ok)
			}
		})
	}
}
