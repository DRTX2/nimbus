import { cleanup, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { setupServer } from "msw/node";
import { createMemoryRouter, RouterProvider } from "react-router";
import { createQueryClient, Providers } from "@/app/providers";
import { routes } from "@/app/router";

export const server = setupServer();

export const principals = {
  owner: { id: "p-owner", tenantId: "t-1", name: "Olga Owner", role: "owner", permissions: ["workflow:read", "workflow:write", "event:write", "execution:read", "analytics:read", "pipeline:read", "pipeline:write", "incident:read", "incident:ack", "incident:classify", "incident:resolve", "approval:read", "approval:decide", "identity:read", "identity:write", "audit:read"] },
  viewer: { id: "p-viewer", tenantId: "t-1", name: "Vic Viewer", role: "viewer", permissions: ["workflow:read", "execution:read", "analytics:read", "pipeline:read", "incident:read", "approval:read"] },
  approver: { id: "p-approver", tenantId: "t-1", name: "Ada Approver", role: "approver", permissions: ["workflow:read", "execution:read", "analytics:read", "incident:read", "approval:read", "approval:decide"] },
};

/**
 * Renders the real route tree at `path`, optionally already signed in, so tests
 * exercise routing, session handling, data fetching and components together.
 */
export function renderApp(path: string, { signedIn = true }: { signedIn?: boolean } = {}) {
  if (signedIn) sessionStorage.setItem("nimbus.credential", "test-credential");
  else sessionStorage.clear();
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const client = createQueryClient();
  client.setDefaultOptions({ queries: { ...client.getDefaultOptions().queries, retry: false, refetchInterval: false } });
  const user = userEvent.setup();
  const view = render(
    <Providers client={client}>
      <RouterProvider router={router} />
    </Providers>,
  );
  return { ...view, user, router };
}

/** Unmounts the app and clears mocks and storage between scenarios. */
export function resetScenario(): void {
  cleanup();
  server.resetHandlers();
  sessionStorage.clear();
  localStorage.clear();
}
