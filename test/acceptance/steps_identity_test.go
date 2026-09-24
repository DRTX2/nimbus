//go:build acceptance

package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

var errRemoteUnsupported = errors.New("step requires the in-process environment")

func registerIdentitySteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a fresh tenant with an owner$`, func(ctx context.Context) error {
		tenantID, token, err := w.env.provisionTenant(ctx)
		if err != nil {
			return err
		}
		w.tenantID, w.tokens["owner"], w.actor = tenantID, token, "owner"
		w.vars["tenantId"] = tenantID
		return nil
	})
	sc.Step(`^I am signed in as "([^"]*)"$`, func(actor string) error {
		if _, ok := w.tokens[actor]; !ok {
			return fmt.Errorf("no credential for %q", actor)
		}
		w.actor = actor
		return nil
	})
	sc.Step(`^I am signed in as the owner$`, func() error {
		w.actor = "owner"
		return nil
	})
	sc.Step(`^the owner issues an? "(owner|operator|approver|viewer)" API key for "([^"]*)"$`, func(ctx context.Context, role, name string) error {
		if err := w.requestAs(ctx, "owner", http.MethodPost, "/v1/api-keys", map[string]any{"name": name, "role": role, "expiresInDays": 1}, nil); err != nil {
			return err
		}
		if err := w.expectStatus(http.StatusCreated); err != nil {
			return err
		}
		token, err := w.stringField("token")
		if err != nil {
			return err
		}
		keyID, err := w.stringField("id")
		if err != nil {
			return err
		}
		w.tokens[name], w.vars[name+".keyId"] = token, keyID
		return nil
	})
	sc.Step(`^the owner revokes the API key of "([^"]*)"$`, func(ctx context.Context, name string) error {
		if err := w.requestAs(ctx, "owner", http.MethodDelete, "/v1/api-keys/"+w.vars[name+".keyId"], nil, nil); err != nil {
			return err
		}
		return w.expectStatus(http.StatusNoContent)
	})
	sc.Step(`^"([^"]*)" signs in through the identity provider$`, func(ctx context.Context, user string) error {
		if err := w.mintToken(user, tokenOptions{}); err != nil {
			return err
		}
		// A dashboard login resolves the principal immediately, which is also
		// when Nimbus provisions the membership.
		return w.request(ctx, http.MethodGet, "/v1/me", nil, nil)
	})
	sc.Step(`^"([^"]*)" presents an? (expired|untrusted|wrong-audience) identity token$`, func(user, kind string) error {
		options := tokenOptions{expired: kind == "expired", untrusted: kind == "untrusted"}
		if kind == "wrong-audience" {
			options.audience = "someone-else"
		}
		return w.mintToken(user, options)
	})
	sc.Step(`^the owner changes the role of "([^"]*)" to "(owner|operator|approver|viewer)"$`, func(ctx context.Context, user, role string) error {
		memberID, err := w.memberID(ctx, user)
		if err != nil {
			return err
		}
		if err := w.requestAs(ctx, "owner", http.MethodPatch, "/v1/members/"+memberID, map[string]string{"role": role, "status": "active"}, nil); err != nil {
			return err
		}
		return w.expectStatus(http.StatusOK)
	})
	sc.Step(`^the owner suspends "([^"]*)"$`, func(ctx context.Context, user string) error {
		memberID, err := w.memberID(ctx, user)
		if err != nil {
			return err
		}
		if err := w.requestAs(ctx, "owner", http.MethodPatch, "/v1/members/"+memberID, map[string]string{"role": "viewer", "status": "revoked"}, nil); err != nil {
			return err
		}
		return w.expectStatus(http.StatusOK)
	})
	sc.Step(`^I try to change my own role to "(owner|operator|approver|viewer)"$`, func(ctx context.Context, role string) error {
		if err := w.request(ctx, http.MethodGet, "/v1/me", nil, nil); err != nil {
			return err
		}
		principalID, err := w.stringField("id")
		if err != nil {
			return err
		}
		if err := w.request(ctx, http.MethodGet, "/v1/members", nil, nil); err != nil {
			return err
		}
		items, err := w.field("items")
		if err != nil {
			return err
		}
		for _, item := range items.([]any) {
			entry, _ := item.(map[string]any)
			if entry["principalId"] == principalID {
				return w.request(ctx, http.MethodPatch, "/v1/members/"+entry["id"].(string), map[string]string{"role": role, "status": "active"}, nil)
			}
		}
		return fmt.Errorf("own membership not found")
	})
	sc.Step(`^my role should be "([^"]*)"$`, func(ctx context.Context, role string) error {
		if err := w.request(ctx, http.MethodGet, "/v1/me", nil, nil); err != nil {
			return err
		}
		if err := w.expectStatus(http.StatusOK); err != nil {
			return err
		}
		got, err := w.stringField("role")
		if err != nil {
			return err
		}
		if got != role {
			return fmt.Errorf("role = %q, want %q", got, role)
		}
		return nil
	})
	sc.Step(`^the audit log should record "([^"]*)"$`, func(ctx context.Context, action string) error {
		if err := w.requestAs(ctx, "owner", http.MethodGet, "/v1/audit-events", nil, nil); err != nil {
			return err
		}
		if err := w.expectStatus(http.StatusOK); err != nil {
			return err
		}
		items, err := w.field("items")
		if err != nil {
			return err
		}
		for _, item := range items.([]any) {
			if entry, ok := item.(map[string]any); ok && entry["action"] == action {
				return nil
			}
		}
		return fmt.Errorf("audit log has no %q entry", action)
	})
}

func (w *world) mintToken(user string, options tokenOptions) error {
	if w.env.remote {
		return errRemoteUnsupported
	}
	// A fresh token per sign-in mirrors a real login and bypasses session caches.
	token, err := w.env.idp.mint(user+"-"+w.scenarioID, w.tenantID, options)
	if err != nil {
		return err
	}
	w.tokens[user] = token
	w.actor = user
	return nil
}

func (w *world) memberID(ctx context.Context, user string) (string, error) {
	if err := w.requestAs(ctx, "owner", http.MethodGet, "/v1/members", nil, nil); err != nil {
		return "", err
	}
	if err := w.expectStatus(http.StatusOK); err != nil {
		return "", err
	}
	items, err := w.field("items")
	if err != nil {
		return "", err
	}
	for _, item := range items.([]any) {
		entry, _ := item.(map[string]any)
		if entry["displayName"] == user+"-"+w.scenarioID {
			return entry["id"].(string), nil
		}
	}
	return "", fmt.Errorf("member %q not found in %s", user, truncate(w.body))
}
