import { describeFeature, loadFeature } from "@amiceli/vitest-cucumber";
import { screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { incident, pipeline, signedInAs } from "@/test/fixtures";
import { renderApp, resetScenario, server } from "@/test/render";

const feature = await loadFeature("src/features/incidents/incident-response.feature");

describeFeature(feature, ({ Background, Scenario, AfterEachScenario }) => {
  AfterEachScenario(resetScenario);
  let app: ReturnType<typeof renderApp>;
  let acknowledged: string | null = null;

  Background(({ Given }) => {
    Given("a stalled-consumer incident is open", () => {
      acknowledged = null;
      server.use(
        http.get("*/v1/incidents/:id", () => HttpResponse.json(incident)),
        http.get("*/v1/pipelines/:id", () => HttpResponse.json(pipeline)),
        http.post("*/v1/incidents/:id/acknowledgements", ({ params }) => {
          acknowledged = String(params.id);
          return HttpResponse.json({ status: "acknowledged" });
        }),
      );
    });
  });

  const openIncident = async () => {
    app = renderApp(`/incidents/${incident.id}`);
    await screen.findByRole("heading", { level: 1, name: "Consumer stalled" });
    await screen.findByRole("listitem", { name: /^Queue/ });
  };

  Scenario("The failing stages are highlighted", ({ Given, When, Then, And }) => {
    Given('I am signed in as "owner"', () => server.use(...signedInAs("owner")));
    When("I open the incident", openIncident);
    Then('the "Queue" and "Consumer" stages are implicated', () => {
      expect(screen.getByRole("listitem", { name: "Queue, implicated" })).toBeInTheDocument();
      expect(screen.getByRole("listitem", { name: "Consumer, implicated" })).toBeInTheDocument();
    });
    And('the "Dead letters" stage is not implicated', () => {
      expect(screen.getByRole("listitem", { name: "Dead letters" })).toBeInTheDocument();
    });
  });

  Scenario("An operator acknowledges the incident", ({ Given, When, And, Then }) => {
    Given('I am signed in as "owner"', () => server.use(...signedInAs("owner")));
    When("I open the incident", openIncident);
    And('I press "Acknowledge"', async () => {
      await app.user.click(screen.getByRole("button", { name: "Acknowledge" }));
    });
    Then("the API receives an acknowledgement for the incident", async () => {
      await expect.poll(() => acknowledged).toBe(incident.id);
    });
  });

  Scenario("A viewer can read but not act", ({ Given, When, Then, And }) => {
    Given('I am signed in as "viewer"', () => server.use(...signedInAs("viewer")));
    When("I open the incident", openIncident);
    Then('I do not see the "Acknowledge" action', () => {
      expect(screen.queryByRole("button", { name: "Acknowledge" })).not.toBeInTheDocument();
    });
    And('I do not see the "Resolve incident" action', () => {
      expect(screen.queryByRole("button", { name: "Resolve incident" })).not.toBeInTheDocument();
    });
    And('I see "Not reviewed yet."', () => {
      expect(screen.getByText("Not reviewed yet.")).toBeInTheDocument();
    });
  });
});
