import { describeFeature, loadFeature } from "@amiceli/vitest-cucumber";
import { screen, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { now, signedInAs } from "@/test/fixtures";
import { renderApp, resetScenario, server } from "@/test/render";

const feature = await loadFeature("src/features/executions/executions.feature");

const run = (id: string) => ({ id, workflowId: "66666666-6666-4666-8666-666666666666", workflowName: "Order audit", status: "succeeded", createdAt: now, finishedAt: now });

describeFeature(feature, ({ Scenario, AfterEachScenario }) => {
  AfterEachScenario(resetScenario);
  let app: ReturnType<typeof renderApp>;
  const cursors: (string | null)[] = [];

  Scenario("Older runs load with the next cursor", ({ Given, When, Then, And }) => {
    Given("I am signed in and there are two pages of runs", () => {
      cursors.length = 0;
      server.use(
        ...signedInAs("viewer"),
        http.get("*/v1/executions", ({ request }) => {
          const cursor = new URL(request.url).searchParams.get("cursor");
          cursors.push(cursor);
          return cursor
            ? HttpResponse.json({ items: [run("77777777-7777-4777-8777-777777777777")] })
            : HttpResponse.json({ items: [run("88888888-8888-4888-8888-888888888888")], nextCursor: "page-2" });
        }),
      );
    });
    When("I open the runs page", () => {
      app = renderApp("/executions");
    });
    Then("I see 1 run", async () => {
      await screen.findByRole("button", { name: "Load older runs" });
      expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(2);
    });
    When("I load older runs", async () => {
      await app.user.click(screen.getByRole("button", { name: "Load older runs" }));
    });
    Then("I see 2 runs", async () => {
      await expect.poll(() => within(screen.getByRole("table")).getAllByRole("row").length).toBe(3);
    });
    And("the second request used the cursor from the first page", () => {
      expect(cursors).toEqual([null, "page-2"]);
    });
  });
});
