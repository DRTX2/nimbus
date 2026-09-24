import { expect } from "@playwright/test";
import { Given, Then, When } from "../fixtures";

Given("I am signed in as the owner", async ({ page, nimbus }) => {
  await page.goto("/app/sign-in");
  await page.getByLabel("API key or access token").fill(nimbus.token("owner"));
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
});

Given("the owner has issued a {string} key for {string}", async ({ nimbus }, role: string, name: string) => {
  await nimbus.issueKey(name, role);
});

Given("I am signed in as {string}", async ({ page, nimbus }, name: string) => {
  await page.goto("/app/sign-in");
  await page.getByLabel("API key or access token").fill(nimbus.token(name));
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
});

When("I open the dashboard without signing in", async ({ page }) => {
  await page.goto("/app/incidents");
});

When("I sign in with the credential {string}", async ({ page }, credential: string) => {
  await page.getByLabel("API key or access token").fill(credential);
  await page.getByRole("button", { name: "Sign in" }).click();
});

When("I sign in with the owner credential", async ({ page, nimbus }) => {
  await page.getByLabel("API key or access token").fill(nimbus.token("owner"));
  await page.getByRole("button", { name: "Sign in" }).click();
});

When("I sign out", async ({ page }) => {
  await page.getByRole("button", { name: "Sign out" }).click();
});

When("I open {string} from the navigation", async ({ page }, label: string) => {
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: new RegExp(`^${label}`) }).click();
});

Then("I should be asked to sign in", async ({ page }) => {
  await expect(page.getByRole("heading", { name: "Sign in to Nimbus" })).toBeVisible();
});

Then("I should see the page {string}", async ({ page }, title: string) => {
  await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
});

Then("I should see the message {string}", async ({ page }, message: string) => {
  await expect(page.getByText(message)).toBeVisible();
});

Then("the navigation should not offer {string}", async ({ page }, label: string) => {
  await expect(page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: label })).toHaveCount(0);
});

Then("I should not see the button {string}", async ({ page }, name: string) => {
  await expect(page.getByRole("button", { name })).toHaveCount(0);
});
