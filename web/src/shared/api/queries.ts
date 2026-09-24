import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "./client";

export type Incident = Schemas["Incident"];
export type Pipeline = Schemas["Pipeline"];
export type Workflow = Schemas["Workflow"];
export type WorkflowDetail = Schemas["WorkflowDetail"];
export type WorkflowDefinition = Schemas["Definition"];
export type Execution = Schemas["Execution"];
export type ExecutionSummary = Schemas["ExecutionSummary"];
export type Approval = Schemas["Approval"];
export type APIKey = Schemas["APIKey"];
export type Member = Schemas["Member"];
export type AuditEvent = Schemas["AuditEvent"];
export type Observation = Schemas["ObservationInputBody"];
export type Role = "owner" | "operator" | "approver" | "viewer";
export type IncidentStatusFilter = "active" | "open" | "acknowledged" | "recovered" | "resolved";
export type ExecutionStatus = "awaiting_approval" | "queued" | "running" | "succeeded" | "failed" | "cancelled";

/** Query keys are centralized so mutations invalidate exactly what they change. */
export const keys = {
  analytics: ["analytics"] as const,
  incidents: (status?: string) => ["incidents", status ?? "all"] as const,
  incident: (id: string) => ["incident", id] as const,
  pipelines: ["pipelines"] as const,
  pipeline: (id: string) => ["pipeline", id] as const,
  workflows: ["workflows"] as const,
  workflow: (id: string) => ["workflow", id] as const,
  executions: (filter: object) => ["executions", filter] as const,
  execution: (id: string) => ["execution", id] as const,
  approvals: ["approvals"] as const,
  apiKeys: ["api-keys"] as const,
  members: ["members"] as const,
  audit: ["audit"] as const,
  quality: (weeksAgo: number) => ["quality", weeksAgo] as const,
};

// Operational lists change because of other people and automations, so they
// refresh when a page opens and keep polling while it stays open.
const live = { refetchInterval: 10_000, refetchOnMount: "always" } as const;

export const useAnalytics = () => useQuery({ queryKey: keys.analytics, queryFn: () => unwrap(api.GET("/v1/analytics/overview")), ...live });

export const useIncidents = (status?: IncidentStatusFilter) =>
  useQuery({
    queryKey: keys.incidents(status),
    queryFn: async () => (await unwrap(api.GET("/v1/incidents", { params: { query: status ? { status } : {} } }))).items ?? [],
    ...live,
  });

export const useIncident = (id: string) =>
  useQuery({ queryKey: keys.incident(id), queryFn: () => unwrap(api.GET("/v1/incidents/{incidentId}", { params: { path: { incidentId: id } } })), enabled: id !== "", ...live });

export const usePipelines = () =>
  useQuery({ queryKey: keys.pipelines, queryFn: async () => (await unwrap(api.GET("/v1/pipelines"))).items ?? [] });

export const usePipeline = (id: string) =>
  useQuery({ queryKey: keys.pipeline(id), queryFn: () => unwrap(api.GET("/v1/pipelines/{pipelineId}", { params: { path: { pipelineId: id } } })), enabled: id !== "" });

export const useWorkflows = () =>
  useQuery({ queryKey: keys.workflows, queryFn: async () => (await unwrap(api.GET("/v1/workflows"))).items ?? [] });

export const useWorkflow = (id: string) =>
  useQuery({ queryKey: keys.workflow(id), queryFn: () => unwrap(api.GET("/v1/workflows/{workflowId}", { params: { path: { workflowId: id } } })), enabled: id !== "" });

export function useExecutions(filter: { status?: ExecutionStatus; workflowId?: string }) {
  return useInfiniteQuery({
    queryKey: keys.executions(filter),
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      unwrap(api.GET("/v1/executions", { params: { query: { limit: 25, ...filter, ...(pageParam ? { cursor: pageParam } : {}) } } })),
    getNextPageParam: (page) => page.nextCursor || undefined,
    ...live,
  });
}

export const useExecution = (id: string) =>
  useQuery({
    queryKey: keys.execution(id),
    queryFn: () => unwrap(api.GET("/v1/executions/{executionId}", { params: { path: { executionId: id } } })),
    enabled: id !== "",
    // Poll only while work is still in flight.
    refetchInterval: (query) => (["queued", "running", "awaiting_approval"].includes(query.state.data?.status ?? "queued") ? 2_000 : false),
  });

export const useApprovals = () =>
  useQuery({ queryKey: keys.approvals, queryFn: async () => (await unwrap(api.GET("/v1/approvals"))).items ?? [], ...live });

export const useApiKeys = () => useQuery({ queryKey: keys.apiKeys, queryFn: async () => (await unwrap(api.GET("/v1/api-keys"))).items ?? [] });

export const useMembers = () => useQuery({ queryKey: keys.members, queryFn: async () => (await unwrap(api.GET("/v1/members"))).items ?? [] });

export const useAuditEvents = () => useQuery({ queryKey: keys.audit, queryFn: async () => (await unwrap(api.GET("/v1/audit-events"))).items ?? [] });

export const useWeeklyQuality = (weeksAgo: number) =>
  useQuery({ queryKey: keys.quality(weeksAgo), queryFn: () => unwrap(api.GET("/v1/reports/incident-quality/weekly", { params: { query: { weeksAgo } } })) });

/** Wraps a mutation and invalidates the listed query prefixes on success. */
function useInvalidatingMutation<TInput, TOutput>(mutationFn: (input: TInput) => Promise<TOutput>, invalidate: (input: TInput) => readonly (readonly unknown[])[]) {
  const client = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: (_, input) => Promise.all(invalidate(input).map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export const useAcknowledgeIncident = () =>
  useInvalidatingMutation(
    (id: string) => unwrap(api.POST("/v1/incidents/{incidentId}/acknowledgements", { params: { path: { incidentId: id } } })),
    (id) => [keys.incident(id), ["incidents"]],
  );

export const useReviewIncident = () =>
  useInvalidatingMutation(
    ({ id, disposition, comment }: { id: string; disposition: Schemas["ReviewIncidentInputBody"]["disposition"]; comment?: string }) =>
      unwrap(api.POST("/v1/incidents/{incidentId}/reviews", { params: { path: { incidentId: id } }, body: { disposition, comment } })),
    ({ id }) => [keys.incident(id), ["incidents"], ["quality"]],
  );

export const useResolveIncident = () =>
  useInvalidatingMutation(
    ({ id, note }: { id: string; note?: string }) =>
      unwrap(api.POST("/v1/incidents/{incidentId}/resolutions", { params: { path: { incidentId: id } }, body: { note } })),
    ({ id }) => [keys.incident(id), ["incidents"]],
  );

export const useCreatePipeline = () =>
  useInvalidatingMutation((body: Schemas["CreatePipelineInputBody"]) => unwrap(api.POST("/v1/pipelines", { body })), () => [keys.pipelines]);

export const useRecordObservation = () =>
  useInvalidatingMutation(
    ({ pipelineId, body }: { pipelineId: string; body: Observation }) =>
      unwrap(api.POST("/v1/pipelines/{pipelineId}/observations", { params: { path: { pipelineId } }, body })),
    () => [["incidents"], keys.analytics],
  );

export const useCheckPipeline = () =>
  useInvalidatingMutation(
    (pipelineId: string) => unwrap(api.POST("/v1/pipelines/{pipelineId}/checks", { params: { path: { pipelineId } } })),
    () => [["incidents"]],
  );

export const useCreateWorkflow = () =>
  useInvalidatingMutation((body: Schemas["CreateWorkflowInputBody"]) => unwrap(api.POST("/v1/workflows", { body })), () => [keys.workflows]);

export const useCreateWorkflowVersion = () =>
  useInvalidatingMutation(
    ({ workflowId, definition }: { workflowId: string; definition: WorkflowDefinition }) =>
      unwrap(api.POST("/v1/workflows/{workflowId}/versions", { params: { path: { workflowId } }, body: definition })),
    ({ workflowId }) => [keys.workflow(workflowId), keys.workflows],
  );

export const useActivateVersion = () =>
  useInvalidatingMutation(
    ({ workflowId, version }: { workflowId: string; version: number }) =>
      unwrap(api.POST("/v1/workflows/{workflowId}/versions/{version}/activations", { params: { path: { workflowId, version } } })),
    ({ workflowId }) => [keys.workflow(workflowId), keys.workflows],
  );

export const useIngestEvent = () =>
  useInvalidatingMutation(
    ({ workflowId, idempotencyKey, type, payload }: { workflowId: string; idempotencyKey: string; type: string; payload: Record<string, unknown> }) =>
      unwrap(
        api.POST("/v1/workflows/{workflowId}/events", {
          params: { path: { workflowId }, header: { "Idempotency-Key": idempotencyKey } },
          body: { type, payload },
        }),
      ),
    () => [["executions"], keys.analytics, keys.approvals],
  );

export const useDecideApproval = () =>
  useInvalidatingMutation(
    ({ id, decision, comment }: { id: string; decision: "approved" | "rejected"; comment?: string }) =>
      unwrap(api.POST("/v1/approvals/{approvalId}/decisions", { params: { path: { approvalId: id } }, body: { decision, comment } })),
    () => [keys.approvals, ["executions"]],
  );

export const useCreateApiKey = () =>
  useInvalidatingMutation((body: Schemas["CreateAPIKeyInputBody"]) => unwrap(api.POST("/v1/api-keys", { body })), () => [keys.apiKeys, keys.members, keys.audit]);

export const useRevokeApiKey = () =>
  useInvalidatingMutation(
    (id: string) => unwrap(api.DELETE("/v1/api-keys/{apiKeyId}", { params: { path: { apiKeyId: id } } })),
    () => [keys.apiKeys, keys.audit],
  );

export const useUpdateMember = () =>
  useInvalidatingMutation(
    ({ id, role, status }: { id: string; role: Role; status: "active" | "revoked" }) =>
      unwrap(api.PATCH("/v1/members/{memberId}", { params: { path: { memberId: id } }, body: { role, status } })),
    () => [keys.members, keys.audit],
  );
