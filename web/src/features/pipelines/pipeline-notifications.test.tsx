import { describeFeature, loadFeature } from "@amiceli/vitest-cucumber";
import { screen, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { now, pipeline, signedInAs } from "@/test/fixtures";
import { renderApp, resetScenario, server } from "@/test/render";

const feature = await loadFeature("src/features/pipelines/pipeline-notifications.feature");
const workflows = [
  { id: "44444444-4444-4444-8444-444444444444", name: "Page on-call", eventType: "nimbus.incident.opened" },
  { id: "55555555-5555-4555-8555-555555555555", name: "All clear", eventType: "nimbus.incident.recovered" },
  { id: "66666666-6666-4666-8666-666666666666", name: "Order audit", eventType: "order.created" },
].map(({ eventType, ...workflow }) => ({ ...workflow, status: "active", version: 1, definition: { eventType, steps: [] }, createdAt: now, updatedAt: now, approvalRequired: false }));
const workflowId = (name: string) => workflows.find((workflow) => workflow.name === name)?.id;

describeFeature(feature, ({ Background, Scenario, AfterEachScenario }) => {
  AfterEachScenario(resetScenario);
  let app: ReturnType<typeof renderApp>;
  let posted: { incidentWorkflowId?: string; recoveryWorkflowId?: string } | null = null;

  Background(({ Given, And }) => {
    Given("workflows exist for opened and recovered incidents", () => {
      posted = null;
      server.use(
        http.get("*/v1/workflows", () => HttpResponse.json({ items: workflows })),
        http.get("*/v1/pipelines", () => HttpResponse.json({ items: [] })),
        http.post("*/v1/pipelines", async ({ request }) => {
          posted = (await request.json()) as typeof posted;
          return HttpResponse.json({ ...pipeline, ...posted }, { status: 201 });
        }),
        http.get("*/v1/pipelines/:id", () => HttpResponse.json(pipeline)),
      );
    });
    And('I am signed in as "owner"', () => server.use(...signedInAs("owner")));
  });

  const startAdding = async () => {
    app = renderApp("/pipelines");
    await app.user.click(await screen.findByRole("button", { name: "Add pipeline" }));
    await screen.findByRole("option", { name: "All clear" });
  };

  Scenario("Opened and recovered incidents are routed to different workflows", ({ When, Then }) => {
    When('I add a test pipeline "Billing" that pages "Page on-call" and reports recovery to "All clear"', async () => {
      await startAdding();
      await app.user.type(screen.getByLabelText("Name"), "Billing");
      await app.user.type(screen.getByLabelText("Queue ARN"), pipeline.queueArn);
      await app.user.type(screen.getByLabelText("Consumer function ARN"), pipeline.lambdaArn);
      await app.user.type(screen.getByLabelText("Dead-letter queue ARN"), pipeline.dlqArn);
      await app.user.selectOptions(screen.getByLabelText("Response workflow"), "Page on-call");
      await app.user.selectOptions(screen.getByLabelText("Recovery workflow"), "All clear");
      await app.user.click(within(screen.getByRole("region", { name: "Add a pipeline" })).getByRole("button", { name: "Add pipeline" }));
    });
    Then('the API receives a pipeline that pages "Page on-call" and reports recovery to "All clear"', async () => {
      await expect.poll(() => posted).toMatchObject({ incidentWorkflowId: workflowId("Page on-call"), recoveryWorkflowId: workflowId("All clear") });
    });
  });

  Scenario("The recovery choice offers only recovery workflows", ({ When, Then }) => {
    When("I start adding a pipeline", startAdding);
    Then('the "Recovery workflow" choices are "None, All clear"', () => {
      const choices = within(screen.getByLabelText("Recovery workflow")).getAllByRole("option").map((option) => option.textContent);
      expect(choices).toEqual(["None", "All clear"]);
    });
  });
});
