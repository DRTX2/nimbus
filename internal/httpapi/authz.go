package httpapi

import (
	"context"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/david/nimbus/internal/store"
)

// Permission is a capability checked by every tenant-facing handler.
type Permission string

const (
	PermWorkflowRead     Permission = "workflow:read"
	PermWorkflowWrite    Permission = "workflow:write"
	PermEventWrite       Permission = "event:write"
	PermExecutionRead    Permission = "execution:read"
	PermAnalyticsRead    Permission = "analytics:read"
	PermPipelineRead     Permission = "pipeline:read"
	PermPipelineWrite    Permission = "pipeline:write"
	PermIncidentRead     Permission = "incident:read"
	PermIncidentAck      Permission = "incident:ack"
	PermIncidentClassify Permission = "incident:classify"
	PermIncidentResolve  Permission = "incident:resolve"
	PermApprovalRead     Permission = "approval:read"
	PermApprovalDecide   Permission = "approval:decide"
	PermIdentityRead     Permission = "identity:read"
	PermIdentityWrite    Permission = "identity:write"
	PermAuditRead        Permission = "audit:read"
)

var readOnly = []Permission{
	PermWorkflowRead, PermExecutionRead, PermAnalyticsRead, PermPipelineRead,
	PermIncidentRead, PermApprovalRead,
}

// rolePermissions is the single source of truth for RBAC. Owners hold every
// permission; the matrix is exposed through /v1/me so clients never duplicate it.
var rolePermissions = map[string][]Permission{
	"operator": append(slices.Clone(readOnly),
		PermWorkflowWrite, PermEventWrite, PermPipelineWrite,
		PermIncidentAck, PermIncidentClassify, PermIncidentResolve),
	"approver": {
		PermWorkflowRead, PermExecutionRead, PermAnalyticsRead,
		PermIncidentRead, PermApprovalRead, PermApprovalDecide,
	},
	"viewer": slices.Clone(readOnly),
}

var allPermissions = []Permission{
	PermWorkflowRead, PermWorkflowWrite, PermEventWrite, PermExecutionRead, PermAnalyticsRead,
	PermPipelineRead, PermPipelineWrite, PermIncidentRead, PermIncidentAck, PermIncidentClassify,
	PermIncidentResolve, PermApprovalRead, PermApprovalDecide, PermIdentityRead, PermIdentityWrite,
	PermAuditRead,
}

func roleAllows(role string, permission Permission) bool {
	if role == "owner" {
		return true
	}
	return slices.Contains(rolePermissions[role], permission)
}

func permissionsFor(role string) []Permission {
	if role == "owner" {
		return slices.Clone(allPermissions)
	}
	return slices.Clone(rolePermissions[role])
}

func requirePermission(ctx context.Context, permission Permission) (store.Principal, error) {
	principal := principalFrom(ctx)
	if principal.ID == "" || !roleAllows(principal.Role, permission) {
		return store.Principal{}, huma.Error403Forbidden("the authenticated principal lacks the required permission")
	}
	return principal, nil
}
