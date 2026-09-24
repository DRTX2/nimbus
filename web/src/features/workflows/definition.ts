import type { WorkflowDefinition } from "@/shared/api/queries";

export const definitionTemplate = `{
  "eventType": "order.created",
  "steps": [
    { "id": "validate", "type": "noop" },
    {
      "id": "notify",
      "type": "http",
      "dependsOn": ["validate"],
      "http": { "url": "https://hooks.example.com/orders", "bodyMode": "event" },
      "retry": { "maxAttempts": 3 }
    }
  ]
}`;

export type ParseResult = { definition: WorkflowDefinition; error?: undefined } | { definition?: undefined; error: string };

/**
 * Parses and pre-checks a definition typed by hand. The server remains the
 * authority; this catches the mistakes people make most often while editing.
 */
export function parseDefinition(source: string): ParseResult {
  let value: unknown;
  try {
    value = JSON.parse(source);
  } catch (error) {
    return { error: `The definition is not valid JSON: ${(error as Error).message}` };
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) return { error: "The definition must be a JSON object." };
  const candidate = value as Partial<WorkflowDefinition>;
  if (typeof candidate.eventType !== "string" || !candidate.eventType.trim()) return { error: "Set eventType to the event that starts this workflow." };
  if (!Array.isArray(candidate.steps) || candidate.steps.length === 0) return { error: "Add at least one step." };
  const ids = new Set<string>();
  for (const step of candidate.steps) {
    if (!step || typeof step.id !== "string" || !/^[a-z][a-z0-9_-]*$/.test(step.id)) {
      return { error: "Each step needs an id of lowercase letters, digits, dashes or underscores, starting with a letter." };
    }
    if (ids.has(step.id)) return { error: `Step id "${step.id}" is used twice.` };
    ids.add(step.id);
  }
  for (const step of candidate.steps) {
    for (const dependency of step.dependsOn ?? []) {
      if (!ids.has(dependency)) return { error: `Step "${step.id}" depends on "${dependency}", which does not exist.` };
    }
  }
  if (hasCycle(candidate.steps)) return { error: "Steps depend on each other in a loop. Remove one dependency to break it." };
  return { definition: candidate as WorkflowDefinition };
}

function hasCycle(steps: { id: string; dependsOn?: string[] | null }[]): boolean {
  const graph = new Map(steps.map((step) => [step.id, step.dependsOn ?? []]));
  const state = new Map<string, "visiting" | "done">();
  const visit = (id: string): boolean => {
    if (state.get(id) === "visiting") return true;
    if (state.get(id) === "done") return false;
    state.set(id, "visiting");
    const cyclic = (graph.get(id) ?? []).some(visit);
    state.set(id, "done");
    return cyclic;
  };
  return steps.some((step) => visit(step.id));
}

/** Groups steps into stages that can run in parallel, for display. */
export function stages(definition: WorkflowDefinition): string[][] {
  const steps = definition.steps ?? [];
  const depth = new Map<string, number>();
  const byId = new Map(steps.map((step) => [step.id, step]));
  const resolve = (id: string, guard = 0): number => {
    const known = depth.get(id);
    if (known !== undefined) return known;
    if (guard > steps.length) return 0;
    const dependencies = byId.get(id)?.dependsOn ?? [];
    const value = dependencies.length === 0 ? 0 : Math.max(...dependencies.map((dependency) => resolve(dependency, guard + 1))) + 1;
    depth.set(id, value);
    return value;
  };
  const result: string[][] = [];
  for (const step of steps) {
    const level = resolve(step.id);
    (result[level] ??= []).push(step.id);
  }
  return result;
}
