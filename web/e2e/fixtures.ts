import { randomUUID } from "node:crypto";
import { expect, type APIRequestContext, type Page } from "@playwright/test";
import { createBdd, test as base } from "playwright-bdd";

const ownerKey = process.env.NIMBUS_E2E_API_KEY ?? "nimbus-local-development-key";

/** Seeds data through the public API so journeys start from a known state. */
export class NimbusApi {
  readonly run = randomUUID().slice(0, 8);
  readonly tokens = new Map<string, string>([["owner", ownerKey]]);
  readonly remembered = new Map<string, string>();

  constructor(private readonly request: APIRequestContext) {}

  token(name: string): string {
    const value = this.tokens.get(name);
    if (!value) throw new Error(`no credential issued for ${name}`);
    return value;
  }

  unique(name: string): string {
    return `${name} ${this.run}`;
  }

  async call<T = Record<string, unknown>>(method: string, path: string, data?: unknown, as = "owner", headers: Record<string, string> = {}): Promise<T> {
    const response = await this.request.fetch(path, {
      method,
      data,
      headers: { Authorization: `Bearer ${this.tokens.get(as)}`, ...headers },
    });
    expect(response.ok(), `${method} ${path} -> ${response.status()} ${await response.text()}`).toBeTruthy();
    return response.status() === 204 ? ({} as T) : ((await response.json()) as T);
  }

  async issueKey(name: string, role: string): Promise<string> {
    const key = await this.call<{ token: string }>("POST", "/v1/api-keys", { name: this.unique(name), role, expiresInDays: 1 });
    this.tokens.set(name, key.token);
    return key.token;
  }

  async createWorkflow(name: string, eventType: string, approvalRequired = false): Promise<string> {
    const workflow = await this.call<{ id: string }>("POST", "/v1/workflows", {
      name: this.unique(name),
      approvalRequired,
      definition: { eventType, steps: [{ id: "validate", type: "noop" }, { id: "record", type: "noop", dependsOn: ["validate"] }] },
    });
    this.remembered.set(`workflow:${name}`, workflow.id);
    return workflow.id;
  }

  async ingest(workflowName: string, eventType: string, as = "owner"): Promise<{ executionId: string; approvalId?: string }> {
    return this.call("POST", `/v1/workflows/${this.remembered.get(`workflow:${workflowName}`)}/events`, { type: eventType, payload: { orderId: this.run } }, as, {
      "Idempotency-Key": randomUUID(),
    });
  }

  async createFixturePipeline(name: string): Promise<string> {
    const arn = (kind: string) => `arn:aws:${kind}:us-east-1:123456789012:${name.toLowerCase()}-${this.run}`;
    const pipeline = await this.call<{ id: string }>("POST", "/v1/pipelines", {
      name: this.unique(name),
      mode: "fixture",
      region: "us-east-1",
      queueArn: arn("sqs"),
      lambdaArn: arn("lambda") + "-fn",
      dlqArn: arn("sqs") + "-dlq",
    });
    this.remembered.set(`pipeline:${name}`, pipeline.id);
    return pipeline.id;
  }

  async rememberPipeline(name: string): Promise<void> {
    const { items } = await this.call<{ items: { id: string; name: string }[] }>("GET", "/v1/pipelines");
    const pipeline = items.find((item) => item.name === this.unique(name));
    if (!pipeline) throw new Error(`pipeline ${name} was not created`);
    this.remembered.set(`pipeline:${name}`, pipeline.id);
  }

  async reportStalledConsumer(pipelineName: string, windowsAgo = [2, 1]): Promise<void> {
    await this.reportWindows(pipelineName, windowsAgo, { visibleMessages: 900, oldestMessageSeconds: 1800, lambdaInvocations: 0 });
  }

  async reportHealthyConsumer(pipelineName: string, windowsAgo: number[]): Promise<void> {
    await this.reportWindows(pipelineName, windowsAgo, { visibleMessages: 3, oldestMessageSeconds: 5, lambdaInvocations: 120 });
  }

  private async reportWindows(pipelineName: string, windowsAgo: number[], metrics: { visibleMessages: number; oldestMessageSeconds: number; lambdaInvocations: number }): Promise<void> {
    const size = 5 * 60 * 1000;
    const end = Math.floor(Date.now() / size) * size;
    for (const offset of windowsAgo) {
      const start = end - (offset + 1) * size;
      await this.call("POST", `/v1/pipelines/${this.remembered.get(`pipeline:${pipelineName}`)}/observations`, {
        windowStartedAt: new Date(start).toISOString(),
        windowEndedAt: new Date(start + size).toISOString(),
        inFlightMessages: 0,
        dlqMessages: 0,
        lambdaErrors: 0,
        lambdaThrottles: 0,
        ...metrics,
      });
    }
  }
}

export const test = base.extend<{ nimbus: NimbusApi; page: Page }>({
  nimbus: async ({ request }, use) => {
    await use(new NimbusApi(request));
  },
  // Any uncaught exception or console error (including CSP violations) fails
  // the journey, so regressions that do not break a click still surface.
  page: async ({ page }, use) => {
    const problems: string[] = [];
    page.on("pageerror", (error) => problems.push(`uncaught: ${error.message}`));
    page.on("console", (message) => {
      // Rejected credentials are an expected 401 in sign-in journeys.
      if (message.type() === "error" && !message.text().includes("401")) problems.push(message.text());
    });
    await use(page);
    expect(problems, "browser errors during the journey").toEqual([]);
  },
});

export const { Given, When, Then } = createBdd(test);
