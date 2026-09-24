//go:build acceptance

package acceptance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

func registerWorkflowSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^I create a workflow "([^"]*)" triggered by "([^"]*)" with steps:$`, func(ctx context.Context, name, eventType string, table *godog.Table) error {
		return w.createWorkflow(ctx, name, eventType, false, table)
	})
	sc.Step(`^I create a protected workflow "([^"]*)" triggered by "([^"]*)" with steps:$`, func(ctx context.Context, name, eventType string, table *godog.Table) error {
		return w.createWorkflow(ctx, name, eventType, true, table)
	})
	sc.Step(`^I try to create a workflow "([^"]*)" triggered by "([^"]*)" with steps:$`, func(ctx context.Context, name, eventType string, table *godog.Table) error {
		definition, err := w.definition(eventType, table)
		if err != nil {
			return err
		}
		return w.request(ctx, http.MethodPost, "/v1/workflows", map[string]any{"name": name, "definition": definition}, nil)
	})
	sc.Step(`^I ingest an? "([^"]*)" event with idempotency key "([^"]*)"$`, func(ctx context.Context, eventType, key string) error {
		return w.ingest(ctx, eventType, key, map[string]any{"orderId": "ord-42"})
	})
	sc.Step(`^I ingest an? "([^"]*)" event with idempotency key "([^"]*)" into workflow "([^"]*)"$`, func(ctx context.Context, eventType, key, workflow string) error {
		w.vars["workflowId"] = w.vars["workflow:"+workflow]
		return w.ingest(ctx, eventType, key, map[string]any{"orderId": "ord-42"})
	})
	sc.Step(`^I ingest (\d+) distinct "([^"]*)" events$`, func(ctx context.Context, count int, eventType string) error {
		w.statuses = nil
		for index := range count {
			if err := w.ingest(ctx, eventType, fmt.Sprintf("burst-%d", index), map[string]any{}); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^(\d+) events should be accepted and the rest rejected with status (\d+)$`, func(accepted, rejected int) error {
		for index, status := range w.statuses {
			want := http.StatusAccepted
			if index >= accepted {
				want = rejected
			}
			if status != want {
				return fmt.Errorf("event %d status = %d, want %d (all: %v)", index, status, want, w.statuses)
			}
		}
		return nil
	})
	sc.Step(`^the ingestion should be accepted as (new|a duplicate)$`, func(kind string) error {
		if err := w.expectStatus(http.StatusAccepted); err != nil {
			return err
		}
		duplicate, err := w.stringField("duplicate")
		if err != nil {
			return err
		}
		if want := strconv.FormatBool(kind == "a duplicate"); duplicate != want {
			return fmt.Errorf("duplicate = %s, want %s", duplicate, want)
		}
		return nil
	})
	sc.Step(`^it should reference the same execution as before$`, func() error {
		got, err := w.stringField("executionId")
		if err != nil {
			return err
		}
		if got != w.vars["firstExecutionId"] {
			return fmt.Errorf("executionId = %s, want %s", got, w.vars["firstExecutionId"])
		}
		return nil
	})
	sc.Step(`^the execution should (succeed|fail|be cancelled|await approval) within (\d+) seconds$`, func(ctx context.Context, outcome string, seconds int) error {
		want := map[string]string{"succeed": "succeeded", "fail": "failed", "be cancelled": "cancelled", "await approval": "awaiting_approval"}[outcome]
		return eventually(ctx, time.Duration(seconds)*time.Second, func() error {
			execution, err := w.execution(ctx)
			if err != nil {
				return err
			}
			if execution.Status != want {
				return fmt.Errorf("execution status = %s, want %s", execution.Status, want)
			}
			return nil
		})
	})
	sc.Step(`^step "([^"]*)" should be "([^"]*)" after (\d+) attempts?$`, func(ctx context.Context, stepID, status string, attempts int) error {
		execution, err := w.execution(ctx)
		if err != nil {
			return err
		}
		for _, step := range execution.Steps {
			if step.StepID == stepID {
				if step.Status != status || step.Attempt != attempts {
					return fmt.Errorf("step %s = %s after %d attempts, want %s after %d", stepID, step.Status, step.Attempt, status, attempts)
				}
				return nil
			}
		}
		return fmt.Errorf("step %s was never scheduled", stepID)
	})
	sc.Step(`^step "([^"]*)" should never have been scheduled$`, func(ctx context.Context, stepID string) error {
		execution, err := w.execution(ctx)
		if err != nil {
			return err
		}
		for _, step := range execution.Steps {
			if step.StepID == stepID {
				return fmt.Errorf("step %s was scheduled with status %s", stepID, step.Status)
			}
		}
		return nil
	})
	sc.Step(`^steps "([^"]*)" and "([^"]*)" should both start after "([^"]*)" finished$`, func(ctx context.Context, left, right, dependency string) error {
		execution, err := w.execution(ctx)
		if err != nil {
			return err
		}
		steps := map[string]stepView{}
		for _, step := range execution.Steps {
			steps[step.StepID] = step
		}
		parent := steps[dependency]
		for _, id := range []string{left, right} {
			child := steps[id]
			if parent.FinishedAt == nil || child.StartedAt == nil || child.StartedAt.Before(*parent.FinishedAt) {
				return fmt.Errorf("step %s did not start after %s finished", id, dependency)
			}
		}
		return nil
	})
	sc.Step(`^the webhook "([^"]*)" responds with statuses "([^"]*)"$`, func(hook, statuses string) error {
		if w.env.remote {
			return errRemoteUnsupported
		}
		var codes []int
		for _, value := range strings.Split(statuses, ",") {
			code, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return err
			}
			codes = append(codes, code)
		}
		w.env.receiver.script(w.hook(hook), codes)
		return nil
	})
	sc.Step(`^the webhook "([^"]*)" should have received (\d+) deliver(?:y|ies)$`, func(hook string, count int) error {
		if got := len(w.env.receiver.received(w.hook(hook))); got != count {
			return fmt.Errorf("webhook %s received %d deliveries, want %d", hook, got, count)
		}
		return nil
	})
	sc.Step(`^every delivery to "([^"]*)" should be signed and carry the idempotency key of step "([^"]*)"$`, func(hook, stepID string) error {
		deliveries := w.env.receiver.received(w.hook(hook))
		if len(deliveries) == 0 {
			return fmt.Errorf("webhook %s received nothing", hook)
		}
		for _, delivery := range deliveries {
			if got, want := delivery.header.Get("Idempotency-Key"), w.vars["executionId"]+":"+stepID; got != want {
				return fmt.Errorf("Idempotency-Key = %q, want %q", got, want)
			}
			if !validSignature(delivery) {
				return fmt.Errorf("delivery signature %q is invalid", delivery.header.Get("Nimbus-Signature"))
			}
		}
		return nil
	})
	sc.Step(`^the delivery to "([^"]*)" should contain the event payload$`, func(hook string) error {
		deliveries := w.env.receiver.received(w.hook(hook))
		if len(deliveries) == 0 {
			return fmt.Errorf("webhook %s received nothing", hook)
		}
		var payload map[string]any
		if err := json.Unmarshal(deliveries[0].body, &payload); err != nil || payload["orderId"] != "ord-42" {
			return fmt.Errorf("delivery body %s does not contain the event payload", deliveries[0].body)
		}
		return nil
	})
	sc.Step(`^"([^"]*)" (approves|rejects) the pending approval$`, func(ctx context.Context, actor, decision string) error {
		value := map[string]string{"approves": "approved", "rejects": "rejected"}[decision]
		return w.requestAs(ctx, actor, http.MethodPost, "/v1/approvals/"+w.vars["approvalId"]+"/decisions", map[string]string{"decision": value}, nil)
	})
}

func (w *world) hook(name string) string { return w.scenarioID + "-" + name }

func (w *world) definition(eventType string, table *godog.Table) (map[string]any, error) {
	header := table.Rows[0].Cells
	steps := make([]map[string]any, 0, len(table.Rows)-1)
	for _, row := range table.Rows[1:] {
		step := map[string]any{}
		for index, cell := range row.Cells {
			value := strings.TrimSpace(cell.Value)
			if value == "" {
				continue
			}
			switch header[index].Value {
			case "id", "type":
				step[header[index].Value] = value
			case "dependsOn":
				step["dependsOn"] = strings.Split(value, ",")
			case "webhook":
				target := "http://127.0.0.1:9/unreachable"
				if !w.env.remote {
					target = w.env.receiver.url(w.hook(value))
				}
				step["http"] = map[string]any{"url": target, "method": "POST", "bodyMode": "event"}
			case "url":
				step["http"] = map[string]any{"url": value}
			case "maxAttempts":
				attempts, err := strconv.Atoi(value)
				if err != nil {
					return nil, err
				}
				step["retry"] = map[string]any{"maxAttempts": attempts, "initialDelaySeconds": 1, "maxDelaySeconds": 1}
			default:
				return nil, fmt.Errorf("unknown step column %q", header[index].Value)
			}
		}
		steps = append(steps, step)
	}
	return map[string]any{"schemaVersion": 1, "eventType": eventType, "steps": steps}, nil
}

func (w *world) createWorkflow(ctx context.Context, name, eventType string, protected bool, table *godog.Table) error {
	definition, err := w.definition(eventType, table)
	if err != nil {
		return err
	}
	body := map[string]any{"name": name, "definition": definition, "approvalRequired": protected}
	if err := w.request(ctx, http.MethodPost, "/v1/workflows", body, nil); err != nil {
		return err
	}
	if err := w.expectStatus(http.StatusCreated); err != nil {
		return err
	}
	id, err := w.stringField("id")
	if err != nil {
		return err
	}
	w.vars["workflowId"], w.vars["workflow:"+name] = id, id
	return nil
}

func (w *world) ingest(ctx context.Context, eventType, key string, payload map[string]any) error {
	headers := map[string]string{"Idempotency-Key": w.scenarioID + "-" + key}
	if err := w.request(ctx, http.MethodPost, "/v1/workflows/"+w.vars["workflowId"]+"/events",
		map[string]any{"type": eventType, "payload": payload}, headers); err != nil {
		return err
	}
	if w.status == http.StatusAccepted {
		for _, name := range []string{"executionId", "eventId", "approvalId"} {
			if value, err := w.stringField(name); err == nil && value != "" {
				w.vars[name] = value
			}
		}
		if _, seen := w.vars["firstExecutionId"]; !seen {
			w.vars["firstExecutionId"] = w.vars["executionId"]
		}
	}
	return nil
}

type stepView struct {
	StepID     string     `json:"stepId"`
	Status     string     `json:"status"`
	Attempt    int        `json:"attempt"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

type executionView struct {
	Status string     `json:"status"`
	Steps  []stepView `json:"steps"`
}

func (w *world) execution(ctx context.Context) (executionView, error) {
	if err := w.requestAs(ctx, "owner", http.MethodGet, "/v1/executions/"+w.vars["executionId"], nil, nil); err != nil {
		return executionView{}, err
	}
	if err := w.expectStatus(http.StatusOK); err != nil {
		return executionView{}, err
	}
	var view executionView
	return view, json.Unmarshal(w.body, &view)
}

func validSignature(delivery delivery) bool {
	timestamp := delivery.header.Get("Nimbus-Timestamp")
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(delivery.body)
	return delivery.header.Get("Nimbus-Signature") == "t="+timestamp+",v1="+hex.EncodeToString(mac.Sum(nil))
}
