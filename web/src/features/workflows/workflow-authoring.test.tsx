import { describeFeature, loadFeature } from "@amiceli/vitest-cucumber";
import { fireEvent, screen, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { now, signedInAs } from "@/test/fixtures";
import { renderApp, resetScenario, server } from "@/test/render";

const feature = await loadFeature("src/features/workflows/workflow-authoring.feature");
const createdId = "33333333-3333-4333-8333-333333333333";

describeFeature(feature, ({ Background, Scenario, AfterEachScenario }) => {
  AfterEachScenario(resetScenario);
  let app: ReturnType<typeof renderApp>;
  let posted: { name?: string } | null = null;

  Background(({ Given }) => {
    Given("I am signed in as an operator with no workflows", () => {
      posted = null;
      server.use(
        ...signedInAs("owner"),
        http.get("*/v1/workflows", () => HttpResponse.json({ items: [] })),
        http.post("*/v1/workflows", async ({ request }) => {
          posted = (await request.json()) as { name?: string };
          return HttpResponse.json({ id: createdId, name: posted.name, status: "active", version: 1, definition: { eventType: "order.created", steps: [] }, createdAt: now, updatedAt: now, approvalRequired: false }, { status: 201 });
        }),
        http.get(`*/v1/workflows/${createdId}`, () =>
          HttpResponse.json({ id: createdId, name: "Order audit", status: "active", version: 1, versions: [1], definition: { eventType: "order.created", steps: [{ id: "validate", type: "noop" }] }, createdAt: now, updatedAt: now, approvalRequired: false }),
        ),
      );
    });
  });

  const start = async (name: string) => {
    app = renderApp("/workflows");
    await app.user.click(await screen.findByRole("button", { name: "Create workflow" }));
    await app.user.type(screen.getByLabelText("Name"), name);
  };
  const submit = () => app.user.click(within(screen.getByRole("region", { name: "Create a workflow" })).getByRole("button", { name: "Create workflow" }));
  const setDefinition = (value: string) => {
    fireEvent.change(screen.getByLabelText("Definition"), { target: { value } });
  };

  Scenario("A dependency loop is rejected before submission", ({ When, And, Then }) => {
    When('I start a workflow named "Loop"', () => start("Loop"));
    And('I enter a definition where "a" and "b" depend on each other', () =>
      setDefinition(JSON.stringify({ eventType: "x", steps: [{ id: "a", type: "noop", dependsOn: ["b"] }, { id: "b", type: "noop", dependsOn: ["a"] }] })),
    );
    And("I submit the workflow", submit);
    Then('I am told "Steps depend on each other in a loop. Remove one dependency to break it."', async () => {
      expect(await screen.findByText("Steps depend on each other in a loop. Remove one dependency to break it.")).toBeInTheDocument();
      expect(screen.getByLabelText("Definition")).toHaveAttribute("aria-invalid", "true");
    });
    And("no workflow was sent to the API", () => expect(posted).toBeNull());
  });

  Scenario("Invalid JSON is explained", ({ When, And, Then }) => {
    When('I start a workflow named "Typo"', () => start("Typo"));
    And('I enter the definition "{ not json"', () => setDefinition("{ not json"));
    And("I submit the workflow", submit);
    Then("I am told the definition is not valid JSON", async () => {
      expect(await screen.findByText(/The definition is not valid JSON/)).toBeInTheDocument();
    });
  });

  Scenario("A valid definition is created and opened", ({ When, And, Then }) => {
    When('I start a workflow named "Order audit"', () => start("Order audit"));
    And("I submit the workflow", submit);
    Then('the API receives a workflow named "Order audit"', async () => {
      await expect.poll(() => posted?.name).toBe("Order audit");
    });
    And("I land on the new workflow", async () => {
      expect(await screen.findByRole("heading", { level: 1, name: "Order audit" })).toBeInTheDocument();
      expect(app.router.state.location.pathname).toBe(`/workflows/${createdId}`);
    });
  });
});
