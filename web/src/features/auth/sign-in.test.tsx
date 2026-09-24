import { describeFeature, loadFeature } from "@amiceli/vitest-cucumber";
import { screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { principals, renderApp, resetScenario, server } from "@/test/render";

const feature = await loadFeature("src/features/auth/sign-in.feature");

describeFeature(feature, ({ Scenario, AfterEachScenario }) => {
  AfterEachScenario(resetScenario);
  let app: ReturnType<typeof renderApp>;
  const emptyLists = [http.get("*/v1/incidents", () => HttpResponse.json({ items: [] })), http.get("*/v1/approvals", () => HttpResponse.json({ items: [] }))];

  Scenario("Anonymous visitors are sent to sign in", ({ Given, When, Then }) => {
    Given("I am not signed in", () => sessionStorage.clear());
    When('I open "/incidents"', () => {
      app = renderApp("/incidents", { signedIn: false });
    });
    Then("I see the sign-in form", async () => {
      expect(await screen.findByRole("heading", { name: "Sign in to Nimbus" })).toBeInTheDocument();
    });
  });

  Scenario("A rejected credential explains the problem", ({ Given, And, When, Then }) => {
    Given("I am not signed in", () => sessionStorage.clear());
    And("the API rejects every credential", () => {
      server.use(http.get("*/v1/me", () => HttpResponse.json({ title: "Unauthorized", status: 401 }, { status: 401 })));
    });
    When('I open "/"', () => {
      app = renderApp("/", { signedIn: false });
    });
    And('I sign in with "wrong-key"', async () => {
      await app.user.type(await screen.findByLabelText("API key or access token"), "wrong-key");
      await app.user.click(screen.getByRole("button", { name: "Sign in" }));
    });
    Then('I am told "That credential was not accepted. It may be revoked, expired, or for a different Nimbus."', async () => {
      expect(await screen.findByText("That credential was not accepted. It may be revoked, expired, or for a different Nimbus.")).toBeInTheDocument();
      expect(sessionStorage.getItem("nimbus.credential")).toBeNull();
    });
  });

  Scenario("A valid credential opens the page I asked for", ({ Given, And, When, Then }) => {
    Given("I am not signed in", () => sessionStorage.clear());
    And("the API accepts the owner credential", () => {
      server.use(
        http.get("*/v1/me", ({ request }) =>
          request.headers.get("Authorization") === "Bearer good-key" ? HttpResponse.json(principals.owner) : HttpResponse.json({}, { status: 401 }),
        ),
        ...emptyLists,
      );
    });
    When('I open "/incidents"', () => {
      app = renderApp("/incidents", { signedIn: false });
    });
    And('I sign in with "good-key"', async () => {
      await app.user.type(await screen.findByLabelText("API key or access token"), "good-key");
      await app.user.click(screen.getByRole("button", { name: "Sign in" }));
    });
    Then('I see the heading "Incidents"', async () => {
      expect(await screen.findByRole("heading", { level: 1, name: "Incidents" })).toBeInTheDocument();
    });
    And("the credential is kept for this tab only", () => {
      expect(sessionStorage.getItem("nimbus.credential")).toBe("good-key");
      expect(localStorage.length).toBe(0);
    });
  });
});
