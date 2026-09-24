import "@testing-library/jest-dom/vitest";
import { afterAll, beforeAll } from "vitest";
import { server } from "./render";

// Gherkin steps run as separate tests, so per-scenario cleanup happens in
// AfterEachScenario (see resetScenario) rather than afterEach.
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
