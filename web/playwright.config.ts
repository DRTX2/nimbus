import { defineConfig, devices } from "@playwright/test";
import { defineBddConfig } from "playwright-bdd";

// End-to-end journeys run against a real Nimbus (API + worker + PostgreSQL)
// serving the built dashboard. `make e2e` starts that stack with Compose.
const baseURL = process.env.NIMBUS_E2E_BASE_URL ?? "http://127.0.0.1:8080";

const testDir = defineBddConfig({
  features: "e2e/features/*.feature",
  steps: ["e2e/fixtures.ts", "e2e/steps/*.ts"],
});

export default defineConfig({
  testDir,
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
    { name: "mobile", use: { ...devices["Pixel 7"] }, grep: /@mobile/ },
  ],
});
