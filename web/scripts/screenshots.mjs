// Seeds a fresh local Nimbus with demo data through the public API and
// captures the dashboard screenshots in docs/images.
// Usage: make dev-split (on an empty database), then
//   node web/scripts/screenshots.mjs
import { mkdir } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { chromium } from "@playwright/test";

const baseURL = process.env.NIMBUS_E2E_BASE_URL ?? "http://127.0.0.1:8080";
const token = process.env.NIMBUS_E2E_API_KEY ?? "nimbus-local-development-key";
const out = new URL("../../docs/images/", import.meta.url);

async function call(method, path, body, headers = {}) {
  const response = await fetch(baseURL + path, {
    method,
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", ...headers },
    body: body && JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`${method} ${path} -> ${response.status} ${await response.text()}`);
  return response.status === 204 ? {} : response.json();
}

const workflow = (name, eventType, steps) => call("POST", "/v1/workflows", { name, definition: { eventType, steps } });
const noop = (id, dependsOn) => ({ id, type: "noop", ...(dependsOn && { dependsOn }) });

const pageOnCall = await workflow("Page on-call", "nimbus.incident.opened", [noop("triage"), noop("page", ["triage"])]);
const allClear = await workflow("All clear", "nimbus.incident.recovered", [noop("notify")]);
const orders = await workflow("Order operations", "order.created", [noop("validate"), noop("reserve-stock", ["validate"]), noop("notify", ["validate"]), noop("audit", ["reserve-stock", "notify"])]);
let lastExecution;
for (const orderId of ["ord-1041", "ord-1042", "ord-1043"]) {
  lastExecution = await call("POST", `/v1/workflows/${orders.id}/events`, { type: "order.created", payload: { orderId } }, { "Idempotency-Key": randomUUID() });
}

async function pipeline(name, windows) {
  const slug = name.toLowerCase().replaceAll(" ", "-");
  const arn = (service, suffix = "") => `arn:aws:${service}:us-east-1:123456789012:${slug}${suffix}`;
  const created = await call("POST", "/v1/pipelines", {
    name, mode: "fixture", region: "us-east-1", queueArn: arn("sqs"), lambdaArn: arn("lambda", "-consumer"), dlqArn: arn("sqs", "-dlq"),
    incidentWorkflowId: pageOnCall.id, recoveryWorkflowId: allClear.id,
  });
  const size = 5 * 60 * 1000;
  const end = Math.floor(Date.now() / size) * size;
  for (const [index, metrics] of windows.entries()) {
    const start = end - (windows.length - index) * size;
    await call("POST", `/v1/pipelines/${created.id}/observations`, {
      windowStartedAt: new Date(start).toISOString(), windowEndedAt: new Date(start + size).toISOString(),
      inFlightMessages: 4, dlqMessages: 0, lambdaErrors: 0, lambdaThrottles: 0, ...metrics,
    });
  }
  return created;
}

const healthy = { visibleMessages: 12, oldestMessageSeconds: 8, lambdaInvocations: 240 };
const stalled = { visibleMessages: 1840, oldestMessageSeconds: 2100, lambdaInvocations: 0, inFlightMessages: 0 };
const billing = await pipeline("Billing", [healthy, healthy, stalled, stalled]);
await pipeline("Order fulfillment", [healthy, { ...healthy, dlqMessages: 3 }, { ...healthy, dlqMessages: 17 }, { ...healthy, dlqMessages: 42 }]);
await pipeline("Email notifications", [healthy, healthy, healthy, healthy]);
await new Promise((resolve) => setTimeout(resolve, 3_000)); // let the worker dispatch incident workflows

const { items: incidents } = await call("GET", "/v1/incidents");
const billingIncident = incidents.find((incident) => incident.pipelineId === billing.id);

await mkdir(out, { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" });
await page.goto(baseURL + "/app/sign-in");
await page.getByLabel("API key or access token").fill(token);
await page.getByRole("button", { name: "Sign in" }).click();
await page.getByRole("navigation", { name: "Primary" }).waitFor();

for (const [file, path] of [
  ["overview.png", "/app/"],
  ["incident.png", `/app/incidents/${billingIncident.id}`],
  ["pipeline.png", `/app/pipelines/${billing.id}`],
  ["workflow.png", `/app/workflows/${orders.id}`],
  ["run.png", `/app/executions/${lastExecution.executionId}`],
]) {
  await page.goto(baseURL + path);
  await page.waitForLoadState("networkidle");
  await page.screenshot({ path: new URL(file, out).pathname });
  process.stdout.write(`docs/images/${file}\n`);
}
await browser.close();
