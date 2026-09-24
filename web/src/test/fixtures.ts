import { http, HttpResponse } from "msw";
import { principals } from "./render";

export const now = new Date().toISOString();

export const pipeline = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "Billing",
  mode: "fixture",
  region: "us-east-1",
  queueArn: "arn:aws:sqs:us-east-1:123456789012:billing",
  lambdaArn: "arn:aws:lambda:us-east-1:123456789012:function:billing-consumer",
  dlqArn: "arn:aws:sqs:us-east-1:123456789012:billing-dlq",
  status: "active",
  consecutiveWindows: 2,
  backlogThreshold: 100,
  ageThresholdSeconds: 300,
  createdAt: now,
};

export const incident = {
  id: "22222222-2222-4222-8222-222222222222",
  pipelineId: pipeline.id,
  pipelineName: pipeline.name,
  detector: "consumer_stalled",
  status: "open",
  severity: "critical",
  summary: "Queue backlog is aging while the Lambda consumer has no invocations",
  evidence: { visibleMessages: 900, oldestMessageSeconds: 1800, lambdaInvocations: 0, dlqMessages: 0 },
  openedAt: now,
  timeline: [{ id: "t1", type: "opened", details: {}, createdAt: now }],
};

/** Handlers every signed-in page needs: identity and the navigation badges. */
export function signedInAs(principal: keyof typeof principals) {
  return [
    http.get("*/v1/me", () => HttpResponse.json(principals[principal])),
    http.get("*/v1/incidents", () => HttpResponse.json({ items: [] })),
    http.get("*/v1/approvals", () => HttpResponse.json({ items: [] })),
  ];
}
