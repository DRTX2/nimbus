import { describeFeature, loadFeature } from "@amiceli/vitest-cucumber";
import { screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { now, signedInAs } from "@/test/fixtures";
import { principals, renderApp, resetScenario, server } from "@/test/render";

const feature = await loadFeature("src/features/approvals/approvals.feature");

function pendingApproval(requestedBy: string) {
  return { id: "44444444-4444-4444-8444-444444444444", executionId: "55555555-5555-4555-8555-555555555555", status: "pending", requestedBy, expiresAt: new Date(Date.now() + 3_600_000).toISOString(), createdAt: now };
}

describeFeature(feature, ({ Scenario, AfterEachScenario }) => {
  AfterEachScenario(resetScenario);
  let app: ReturnType<typeof renderApp>;
  let decision: string | null = null;

  const approverWith = (requestedBy: string) => {
    decision = null;
    server.use(
      http.get("*/v1/approvals", () => HttpResponse.json({ items: [pendingApproval(requestedBy)] })),
      http.post("*/v1/approvals/:id/decisions", async ({ request }) => {
        decision = ((await request.json()) as { decision: string }).decision;
        return HttpResponse.json({ ...pendingApproval(requestedBy), status: decision });
      }),
    );
  };

  Scenario("Someone else's run can be approved", ({ Given, And, When, Then }) => {
    Given("I am signed in as an approver", () => server.use(...signedInAs("approver")));
    And("a run requested by someone else is pending", () => approverWith("p-operator"));
    When("I open the approvals page", async () => {
      app = renderApp("/approvals");
      await screen.findByRole("heading", { level: 1, name: "Approvals" });
    });
    And("I approve the run", async () => {
      await app.user.click(await screen.findByRole("button", { name: "Approve" }));
    });
    Then('the API records an "approved" decision', async () => {
      await expect.poll(() => decision).toBe("approved");
    });
  });

  Scenario("My own run cannot be approved by me", ({ Given, And, When, Then }) => {
    Given("I am signed in as an approver", () => server.use(...signedInAs("approver")));
    And("a run I requested is pending", () => approverWith(principals.approver.id));
    When("I open the approvals page", async () => {
      app = renderApp("/approvals");
      await screen.findByRole("heading", { level: 1, name: "Approvals" });
    });
    Then('I see "You requested this run"', async () => {
      expect(await screen.findByText("You requested this run")).toBeInTheDocument();
    });
    And('there is no "Approve" button', () => {
      expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
    });
  });
});
