import { expect } from "@playwright/test";
import { Given, Then, When } from "../fixtures";

Given("a test pipeline {string} whose consumer has stalled", async ({ nimbus }, name: string) => {
  await nimbus.createFixturePipeline(name);
  await nimbus.reportStalledConsumer(name);
});

Given("a workflow {string} that starts on {string}", async ({ nimbus }, name: string, eventType: string) => {
  await nimbus.createWorkflow(name, eventType);
});

Given("a protected workflow {string} that {string} ran on {string}", async ({ nimbus }, name: string, requester: string, eventType: string) => {
  await nimbus.createWorkflow(name, eventType, true);
  const result = await nimbus.ingest(name, eventType, requester);
  nimbus.remembered.set("executionId", result.executionId);
});

When("I open the incident for pipeline {string}", async ({ page, nimbus }, name: string) => {
  await page.goto("/app/incidents");
  const row = page.getByRole("row").filter({ hasText: nimbus.unique(name) });
  await row.getByRole("link", { name: "Consumer stalled" }).click();
});

Then("the consumer stage should be marked as implicated", async ({ page }) => {
  await expect(page.getByRole("listitem", { name: "Consumer, implicated" })).toBeVisible();
  await expect(page.getByRole("listitem", { name: "Dead letters", exact: true })).toBeVisible();
});

When("I acknowledge the incident", async ({ page }) => {
  await page.getByRole("button", { name: "Acknowledge" }).click();
});

When("I classify it as {string}", async ({ page }, label: string) => {
  await page.getByLabel(label).check();
  await page.getByRole("button", { name: /Save review|Reclassify/ }).click();
});

When("I resolve it noting {string}", async ({ page }, note: string) => {
  await page.getByLabel("Resolution note").fill(note);
  await page.getByRole("button", { name: "Resolve incident" }).click();
});

Then("the incident status should read {string}", async ({ page }, status: string) => {
  await expect(page.locator(".page-header").getByText(status, { exact: true })).toBeVisible();
});

Then("the timeline should show {string}", async ({ page }, entries: string) => {
  const timeline = page.getByRole("region", { name: "Timeline" });
  for (const entry of entries.split(",").map((value) => value.trim())) {
    await expect(timeline.getByText(entry, { exact: true })).toBeVisible();
  }
});

When("I open the workflow {string}", async ({ page, nimbus }, name: string) => {
  await page.goto(`/app/workflows/${nimbus.remembered.get(`workflow:${name}`)}`);
});

When("I send a test event", async ({ page }) => {
  await page.getByRole("button", { name: "Send event" }).click();
});

Then("the run should finish as {string}", async ({ page }, status: string) => {
  await expect(page.getByRole("heading", { level: 1, name: /^Run / })).toBeVisible();
  await expect(page.locator(".page-header").getByText(status, { exact: true })).toBeVisible({ timeout: 20_000 });
});

Then("every step should show {string}", async ({ page }, status: string) => {
  const rows = page.getByRole("region", { name: "Steps" }).getByRole("row");
  await expect(rows).toHaveCount(3);
  await expect(rows.filter({ hasText: status })).toHaveCount(2);
});

When("I start a workflow named {string}", async ({ page, nimbus }, name: string) => {
  await page.getByRole("button", { name: "Create workflow" }).click();
  await page.getByLabel("Name").fill(nimbus.unique(name));
});

When("I submit the workflow", async ({ page }) => {
  await page.getByRole("region", { name: "Create a workflow" }).getByRole("button", { name: "Create workflow" }).click();
});

When("I break the definition with a dependency loop", async ({ page }) => {
  await page.getByLabel("Definition").fill(
    JSON.stringify({ eventType: "loop.test", steps: [{ id: "a", type: "noop", dependsOn: ["b"] }, { id: "b", type: "noop", dependsOn: ["a"] }] }),
  );
});

When("I approve the pending run", async ({ page, nimbus }) => {
  const row = page.getByRole("row").filter({ has: page.locator(`a[href$="${nimbus.remembered.get("executionId")}"]`) });
  await row.getByRole("button", { name: "Approve" }).click();
});

Then("the pending run should show {string}", async ({ page, nimbus }, text: string) => {
  const row = page.getByRole("row").filter({ has: page.locator(`a[href$="${nimbus.remembered.get("executionId")}"]`) });
  await expect(row.getByText(text)).toBeVisible();
});

When("I issue a {string} key named {string}", async ({ page, nimbus }, role: string, name: string) => {
  await page.getByLabel("Name").fill(nimbus.unique(name));
  await page.getByLabel("Role").selectOption(role);
  await page.getByRole("button", { name: "Issue key" }).click();
});

Then("the new key should be shown once", async ({ page, nimbus }) => {
  const token = page.getByTestId("issued-token");
  await expect(token).toHaveText(/^nmb_/);
  nimbus.tokens.set("issued", (await token.textContent()) ?? "");
});

When("I revoke the key named {string}", async ({ page, nimbus }, name: string) => {
  const row = page.getByRole("row").filter({ hasText: nimbus.unique(name) });
  await row.getByRole("button", { name: "Revoke" }).click();
  await expect(row.getByText("Revoked")).toBeVisible();
});

Then("the issued key should no longer work", async ({ request, nimbus }) => {
  const response = await request.get("/v1/me", { headers: { Authorization: `Bearer ${nimbus.tokens.get("issued")}` } });
  expect(response.status()).toBe(401);
});
