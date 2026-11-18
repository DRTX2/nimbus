//go:build acceptance

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// Metric profiles are expressed as domain conditions so features describe
// pipeline behavior rather than raw CloudWatch numbers.
var metricProfiles = map[string]map[string]int64{
	"healthy":        {"visibleMessages": 3, "inFlightMessages": 2, "oldestMessageSeconds": 5, "dlqMessages": 0, "lambdaInvocations": 60, "lambdaErrors": 0, "lambdaThrottles": 0},
	"stalled":        {"visibleMessages": 800, "inFlightMessages": 0, "oldestMessageSeconds": 1200, "dlqMessages": 0, "lambdaInvocations": 0, "lambdaErrors": 0, "lambdaThrottles": 0},
	"dead-lettering": {"visibleMessages": 2, "inFlightMessages": 1, "oldestMessageSeconds": 10, "dlqMessages": 7, "lambdaInvocations": 40, "lambdaErrors": 1, "lambdaThrottles": 0},
	"throttled":      {"visibleMessages": 150, "inFlightMessages": 10, "oldestMessageSeconds": 60, "dlqMessages": 0, "lambdaInvocations": 30, "lambdaErrors": 0, "lambdaThrottles": 12},
	"retry-storming": {"visibleMessages": 400, "inFlightMessages": 50, "oldestMessageSeconds": 90, "dlqMessages": 0, "lambdaInvocations": 100, "lambdaErrors": 70, "lambdaThrottles": 0},
}

func registerIncidentSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a fixture pipeline "([^"]*)"$`, func(ctx context.Context, name string) error {
		return w.createPipeline(ctx, name, nil)
	})
	sc.Step(`^a fixture pipeline "([^"]*)" that dispatches the workflow "([^"]*)"$`, func(ctx context.Context, name, workflow string) error {
		return w.createPipeline(ctx, name, map[string]any{"incidentWorkflowId": w.vars["workflow:"+workflow]})
	})
	sc.Step(`^a fixture pipeline "([^"]*)" that dispatches the workflow "([^"]*)" on recovery$`, func(ctx context.Context, name, workflow string) error {
		return w.createPipeline(ctx, name, map[string]any{"recoveryWorkflowId": w.vars["workflow:"+workflow]})
	})
	sc.Step(`^I try to add a fixture pipeline "([^"]*)" that dispatches the workflow "([^"]*)" on recovery$`, func(ctx context.Context, name, workflow string) error {
		if err := w.createPipeline(ctx, name, map[string]any{"recoveryWorkflowId": w.vars["workflow:"+workflow]}); err != nil && w.status == http.StatusCreated {
			return err
		}
		return nil
	})
	sc.Step(`^the webhook "([^"]*)" should receive an? "(opened|recovered)" notification for "([^"]*)" on pipeline "([^"]*)" within (\d+) seconds$`, func(ctx context.Context, hook, event, detector, pipeline string, seconds int) error {
		return eventually(ctx, time.Duration(seconds)*time.Second, func() error {
			for _, delivery := range w.env.receiver.received(w.hook(hook)) {
				var notification map[string]any
				if err := json.Unmarshal(delivery.body, &notification); err != nil {
					return err
				}
				summary, _ := notification["summary"].(string)
				// The source time of the transition, so consumers can order events that arrive late.
				occurredAt, _ := notification["occurredAt"].(string)
				at, timeErr := time.Parse(time.RFC3339Nano, occurredAt)
				if notification["event"] == event && notification["detector"] == detector && notification["pipelineName"] == pipeline &&
					notification["incidentId"] == w.vars["incidentId"] && summary != "" && timeErr == nil && !at.After(time.Now()) {
					return nil
				}
			}
			return fmt.Errorf("webhook %s has no %s %s notification for %s", hook, event, detector, pipeline)
		})
	})
	// The external ID reference is never configured here, so checks fail before
	// any AWS call while still spending the tenant budget.
	sc.Step(`^an AWS pipeline "([^"]*)"$`, func(ctx context.Context, name string) error {
		return w.createAWSPipeline(ctx, name, 2)
	})
	sc.Step(`^an AWS pipeline "([^"]*)" that opens incidents after (\d+) windows?$`, w.createAWSPipeline)
	sc.Step(`^CloudWatch reports (healthy|stalled|dead-lettering|throttled|retry-storming) metrics$`, func(profile string) {
		w.env.cloudwatch.report(metricProfiles[profile])
	})
	sc.Step(`^the CloudWatch scheduler runs$`, func(ctx context.Context) {
		w.env.scheduler.RunOnce(ctx)
	})
	sc.Step(`^the CloudWatch scheduler runs (\d+) times$`, func(ctx context.Context, runs int) {
		for range runs {
			w.env.scheduler.RunOnce(ctx)
		}
	})
	sc.Step(`^CloudWatch should have been called (\d+) times? for the pipeline$`, func(want int) error {
		if got := w.env.cloudwatch.callsFor(w.vars["pipelineId"]); got != want {
			return fmt.Errorf("CloudWatch calls = %d, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^I check the pipeline against CloudWatch (\d+) times?$`, func(ctx context.Context, count int) error {
		w.statuses = nil
		for range count {
			if err := w.request(ctx, http.MethodPost, "/v1/pipelines/"+w.vars["pipelineId"]+"/checks", nil, nil); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^(\d+) checks should be attempted and the rest rejected with status (\d+)$`, func(attempted, rejected int) error {
		for index, status := range w.statuses {
			if (index < attempted) == (status == rejected) {
				return fmt.Errorf("check %d status = %d, want %d only after %d attempts (all: %v)", index, status, rejected, attempted, w.statuses)
			}
		}
		return nil
	})
	sc.Step(`^the pipeline reports (healthy|stalled|dead-lettering|throttled|retry-storming) metrics for the windows starting (\d+) and (\d+) minutes ago$`, func(ctx context.Context, profile string, first, second int) error {
		for _, minutes := range []int{first, second} {
			if err := w.observe(ctx, profile, minutes); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the pipeline reports (healthy|stalled|dead-lettering|throttled|retry-storming) metrics for the window starting (\d+) minutes ago$`, w.observe)
	sc.Step(`^an? "([^"]*)" incident should be (open|acknowledged|recovered|resolved)$`, func(ctx context.Context, detector, status string) error {
		if err := w.request(ctx, http.MethodGet, "/v1/incidents", nil, nil); err != nil {
			return err
		}
		if err := w.expectStatus(http.StatusOK); err != nil {
			return err
		}
		var list struct {
			Items []struct {
				ID       string `json:"id"`
				Detector string `json:"detector"`
				Status   string `json:"status"`
			} `json:"items"`
		}
		if err := json.Unmarshal(w.body, &list); err != nil {
			return err
		}
		for _, item := range list.Items {
			if item.Detector == detector && item.Status == status {
				w.vars["incidentId"] = item.ID
				return nil
			}
		}
		return fmt.Errorf("no %s incident with status %s in %s", detector, status, truncate(w.body))
	})
	sc.Step(`^no incident should be open$`, func(ctx context.Context) error {
		if err := w.request(ctx, http.MethodGet, "/v1/incidents?status=active", nil, nil); err != nil {
			return err
		}
		if err := w.expectStatus(http.StatusOK); err != nil {
			return err
		}
		if items, err := w.field("items"); err != nil || len(items.([]any)) != 0 {
			return fmt.Errorf("unexpected active incidents: %s", truncate(w.body))
		}
		return nil
	})
	sc.Step(`^I acknowledge the incident$`, func(ctx context.Context) error {
		return w.request(ctx, http.MethodPost, "/v1/incidents/"+w.vars["incidentId"]+"/acknowledgements", nil, nil)
	})
	sc.Step(`^I classify the incident as "([^"]*)"$`, func(ctx context.Context, disposition string) error {
		return w.request(ctx, http.MethodPost, "/v1/incidents/"+w.vars["incidentId"]+"/reviews", map[string]string{"disposition": disposition}, nil)
	})
	sc.Step(`^I resolve the incident noting "([^"]*)"$`, func(ctx context.Context, note string) error {
		return w.request(ctx, http.MethodPost, "/v1/incidents/"+w.vars["incidentId"]+"/resolutions", map[string]string{"note": note}, nil)
	})
	sc.Step(`^the incident timeline should read "([^"]*)"$`, func(ctx context.Context, expected string) error {
		if err := w.request(ctx, http.MethodGet, "/v1/incidents/"+w.vars["incidentId"], nil, nil); err != nil {
			return err
		}
		if err := w.expectStatus(http.StatusOK); err != nil {
			return err
		}
		var incident struct {
			Timeline []struct {
				Type string `json:"type"`
			} `json:"timeline"`
		}
		if err := json.Unmarshal(w.body, &incident); err != nil {
			return err
		}
		got := make([]string, 0, len(incident.Timeline))
		for _, event := range incident.Timeline {
			got = append(got, event.Type)
		}
		if strings.Join(got, ", ") != expected {
			return fmt.Errorf("timeline = %q, want %q", strings.Join(got, ", "), expected)
		}
		return nil
	})
	sc.Step(`^the workflow "([^"]*)" should have (\d+) executions? within (\d+) seconds$`, func(ctx context.Context, workflow string, count, seconds int) error {
		return eventually(ctx, time.Duration(seconds)*time.Second, func() error {
			if err := w.request(ctx, http.MethodGet, "/v1/executions?workflowId="+w.vars["workflow:"+workflow], nil, nil); err != nil {
				return err
			}
			if err := w.expectStatus(http.StatusOK); err != nil {
				return err
			}
			items, err := w.field("items")
			if err != nil {
				return err
			}
			if got := len(items.([]any)); got != count {
				return fmt.Errorf("workflow %s has %d executions, want %d", workflow, got, count)
			}
			return nil
		})
	})
}

func (w *world) createPipeline(ctx context.Context, name string, overrides map[string]any) error {
	suffix := w.scenarioID
	body := map[string]any{
		"name": name, "mode": "fixture", "region": "us-east-1", "consecutiveWindows": 2,
		"queueArn":  "arn:aws:sqs:us-east-1:123456789012:" + name + "-" + suffix,
		"lambdaArn": "arn:aws:lambda:us-east-1:123456789012:function:" + name + "-" + suffix,
		"dlqArn":    "arn:aws:sqs:us-east-1:123456789012:" + name + "-dlq-" + suffix,
	}
	for key, value := range overrides {
		body[key] = value
	}
	if err := w.request(ctx, http.MethodPost, "/v1/pipelines", body, nil); err != nil {
		return err
	}
	if err := w.expectStatus(http.StatusCreated); err != nil {
		return err
	}
	id, err := w.stringField("id")
	w.vars["pipelineId"] = id
	return err
}

func (w *world) createAWSPipeline(ctx context.Context, name string, windows int) error {
	return w.createPipeline(ctx, name, map[string]any{
		"mode": "aws", "roleArn": "arn:aws:iam::123456789012:role/nimbus-read",
		"externalIdRef": "aws-external-id/acceptance-unconfigured", "consecutiveWindows": windows,
	})
}

func (w *world) observe(ctx context.Context, profile string, minutesAgo int) error {
	if minutesAgo%5 != 0 || minutesAgo < 5 {
		return fmt.Errorf("windows must start a positive multiple of five minutes ago")
	}
	start := time.Now().UTC().Truncate(5 * time.Minute).Add(-time.Duration(minutesAgo) * time.Minute)
	body := map[string]any{"windowStartedAt": start, "windowEndedAt": start.Add(5 * time.Minute)}
	for key, value := range metricProfiles[profile] {
		body[key] = value
	}
	if err := w.request(ctx, http.MethodPost, "/v1/pipelines/"+w.vars["pipelineId"]+"/observations", body, nil); err != nil {
		return err
	}
	return w.expectStatus(http.StatusAccepted)
}
