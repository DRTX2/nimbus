import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];

/** A problem returned by the API (RFC 9457) or a transport failure. */
export class ApiError extends Error {
  readonly status: number;
  readonly fieldErrors: { location?: string; message?: string }[];

  constructor(status: number, message: string, fieldErrors: ApiError["fieldErrors"] = []) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fieldErrors = fieldErrors;
  }

  /** A message a person can act on, including field-level validation detail. */
  get explanation(): string {
    const details = this.fieldErrors
      .map((error) => [error.location?.replace(/^body\./, ""), error.message].filter(Boolean).join(": "))
      .filter(Boolean);
    return details.length > 0 ? `${this.message} (${details.join("; ")})` : this.message;
  }
}

type CredentialSource = () => string | null;
type UnauthorizedListener = () => void;

let credential: CredentialSource = () => null;
let onUnauthorized: UnauthorizedListener = () => undefined;

/** Wires the client to the session without a circular import. */
export function configureClient(source: CredentialSource, unauthorized: UnauthorizedListener): void {
  credential = source;
  onUnauthorized = unauthorized;
}

const auth: Middleware = {
  onRequest({ request }) {
    const token = credential();
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    return request;
  },
  onResponse({ response }) {
    if (response.status === 401) onUnauthorized();
    return response;
  },
};

// fetch is resolved per call so instrumentation (tests, tracing) installed
// after module load is honored.
export const api = createClient<paths>({ baseUrl: globalThis.location?.origin ?? "", fetch: (request) => globalThis.fetch(request) });
api.use(auth);

type Result<T> = { data?: T; error?: unknown; response: Response };

/** Returns the payload of a successful call or throws a typed ApiError. */
export async function unwrap<T>(call: Promise<Result<T>>): Promise<T> {
  let result: Result<T>;
  try {
    result = await call;
  } catch {
    throw new ApiError(0, "Nimbus is unreachable. Check your connection and try again.");
  }
  const { data, error, response } = result;
  if (response.ok) return data as T;
  const problem = (error ?? {}) as { detail?: string; title?: string; errors?: ApiError["fieldErrors"] };
  throw new ApiError(response.status, problem.detail ?? problem.title ?? `Request failed with status ${response.status}`, problem.errors ?? []);
}
