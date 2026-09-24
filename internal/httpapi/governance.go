package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/david/nimbus/internal/store"
)

func (a *API) registerGovernance(api huma.API) {
	createKey := securedOperation("createAPIKey", http.MethodPost, "/v1/api-keys", "Governance")
	createKey.DefaultStatus = http.StatusCreated
	huma.Register(api, createKey, a.createAPIKey)
	huma.Register(api, securedOperation("listAPIKeys", http.MethodGet, "/v1/api-keys", "Governance"), a.listAPIKeys)
	deleteKey := securedOperation("revokeAPIKey", http.MethodDelete, "/v1/api-keys/{apiKeyId}", "Governance")
	deleteKey.DefaultStatus = http.StatusNoContent
	huma.Register(api, deleteKey, a.revokeAPIKey)
	huma.Register(api, securedOperation("listApprovals", http.MethodGet, "/v1/approvals", "Approvals"), a.listApprovals)
	huma.Register(api, securedOperation("decideApproval", http.MethodPost, "/v1/approvals/{approvalId}/decisions", "Approvals"), a.decideApproval)
	huma.Register(api, securedOperation("listAuditEvents", http.MethodGet, "/v1/audit-events", "Governance"), a.listAuditEvents)
	huma.Register(api, securedOperation("listMembers", http.MethodGet, "/v1/members", "Governance"), a.listMembers)
	huma.Register(api, securedOperation("updateMember", http.MethodPatch, "/v1/members/{memberId}", "Governance"), a.updateMember)
}

type createAPIKeyInput struct {
	Body struct {
		Name          string `json:"name" minLength:"1" maxLength:"120"`
		Role          string `json:"role" enum:"owner,operator,approver,viewer"`
		ExpiresInDays int    `json:"expiresInDays,omitempty" minimum:"1" maximum:"365"`
	}
}
type createdAPIKey struct {
	store.APIKey
	Token string `json:"token"`
}
type createAPIKeyOutput struct{ Body createdAPIKey }

func (a *API) createAPIKey(ctx context.Context, input *createAPIKeyInput) (*createAPIKeyOutput, error) {
	actor, err := requirePermission(ctx, PermIdentityWrite)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Body.Name)
	if name == "" {
		return nil, huma.Error400BadRequest("name is required")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, huma.Error500InternalServerError("could not generate API key")
	}
	token := "nmb_" + base64.RawURLEncoding.EncodeToString(random)
	var expires *time.Time
	if input.Body.ExpiresInDays > 0 {
		value := time.Now().Add(time.Duration(input.Body.ExpiresInDays) * 24 * time.Hour)
		expires = &value
	}
	key, err := a.store.CreateAPIKey(ctx, actor, name, input.Body.Role, token, expires)
	if err != nil {
		return nil, a.internalError("could not create API key", err)
	}
	return &createAPIKeyOutput{Body: createdAPIKey{APIKey: key, Token: token}}, nil
}

type apiKeysOutput struct {
	Body struct {
		Items []store.APIKey `json:"items"`
	}
}

func (a *API) listAPIKeys(ctx context.Context, _ *struct{}) (*apiKeysOutput, error) {
	actor, err := requirePermission(ctx, PermIdentityRead)
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListAPIKeys(ctx, actor.TenantID)
	if err != nil {
		return nil, a.internalError("could not list API keys", err)
	}
	output := &apiKeysOutput{}
	output.Body.Items = items
	return output, nil
}

type revokeAPIKeyInput struct {
	APIKeyID string `path:"apiKeyId" format:"uuid"`
}

func (a *API) revokeAPIKey(ctx context.Context, input *revokeAPIKeyInput) (*struct{}, error) {
	actor, err := requirePermission(ctx, PermIdentityWrite)
	if err != nil {
		return nil, err
	}
	err = a.store.RevokeAPIKey(ctx, actor, input.APIKeyID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("API key was not found")
	}
	if err != nil {
		return nil, a.internalError("could not revoke API key", err)
	}
	return &struct{}{}, nil
}

type approvalsOutput struct {
	Body struct {
		Items []store.Approval `json:"items"`
	}
}

func (a *API) listApprovals(ctx context.Context, _ *struct{}) (*approvalsOutput, error) {
	actor, err := requirePermission(ctx, PermApprovalRead)
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListApprovals(ctx, actor.TenantID)
	if err != nil {
		return nil, a.internalError("could not list approvals", err)
	}
	out := &approvalsOutput{}
	out.Body.Items = items
	return out, nil
}

type decideApprovalInput struct {
	ApprovalID string `path:"approvalId" format:"uuid"`
	Body       struct {
		Decision string `json:"decision" enum:"approved,rejected"`
		Comment  string `json:"comment,omitempty" maxLength:"500"`
	}
}
type approvalOutput struct{ Body store.Approval }

func (a *API) decideApproval(ctx context.Context, input *decideApprovalInput) (*approvalOutput, error) {
	actor, err := requirePermission(ctx, PermApprovalDecide)
	if err != nil {
		return nil, err
	}
	approval, err := a.store.DecideApproval(ctx, actor, input.ApprovalID, input.Body.Decision, input.Body.Comment)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("approval was not found")
	}
	if errors.Is(err, store.ErrForbidden) {
		return nil, huma.Error403Forbidden("requesters cannot approve their own execution")
	}
	if errors.Is(err, store.ErrConflict) {
		return nil, huma.Error409Conflict("approval is no longer pending")
	}
	if err != nil {
		return nil, a.internalError("approval decision could not be recorded", err)
	}
	return &approvalOutput{Body: approval}, nil
}

type auditOutput struct {
	Body struct {
		Items []store.AuditEvent `json:"items"`
	}
}

func (a *API) listAuditEvents(ctx context.Context, _ *struct{}) (*auditOutput, error) {
	actor, err := requirePermission(ctx, PermAuditRead)
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListAuditEvents(ctx, actor.TenantID, 100)
	if err != nil {
		return nil, a.internalError("could not list audit events", err)
	}
	out := &auditOutput{}
	out.Body.Items = items
	return out, nil
}

type membersOutput struct {
	Body struct {
		Items []store.Member `json:"items"`
	}
}

func (a *API) listMembers(ctx context.Context, _ *struct{}) (*membersOutput, error) {
	actor, err := requirePermission(ctx, PermIdentityRead)
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListMembers(ctx, actor.TenantID)
	if err != nil {
		return nil, a.internalError("could not list members", err)
	}
	out := &membersOutput{}
	out.Body.Items = items
	return out, nil
}

type updateMemberInput struct {
	MemberID string `path:"memberId" format:"uuid"`
	Body     struct {
		Role   string `json:"role" enum:"owner,operator,approver,viewer"`
		Status string `json:"status" enum:"active,revoked"`
	}
}

type memberOutput struct{ Body store.Member }

func (a *API) updateMember(ctx context.Context, input *updateMemberInput) (*memberOutput, error) {
	actor, err := requirePermission(ctx, PermIdentityWrite)
	if err != nil {
		return nil, err
	}
	member, err := a.store.UpdateMember(ctx, actor, input.MemberID, input.Body.Role, input.Body.Status)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, huma.Error404NotFound("member was not found")
	case errors.Is(err, store.ErrForbidden):
		return nil, huma.Error403Forbidden("principals cannot change their own membership")
	case errors.Is(err, store.ErrInvalidInput):
		return nil, huma.Error400BadRequest("role or status is invalid")
	case err != nil:
		return nil, a.internalError("could not update member", err)
	}
	return &memberOutput{Body: member}, nil
}
