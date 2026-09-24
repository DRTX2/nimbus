import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useActivateVersion, useCreateWorkflowVersion, useIngestEvent, useWorkflow, type WorkflowDetail } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, ErrorNotice, Field, Guard, Loading, PageHeader, Panel, StatusBadge } from "@/shared/ui/ui";
import { parseDefinition, stages } from "./definition";
import "./workflows.css";

function StageView({ workflow }: { workflow: WorkflowDetail }) {
  const byId = new Map((workflow.definition.steps ?? []).map((step) => [step.id, step]));
  return (
    <ol className="stages">
      {stages(workflow.definition).map((ids, index) => (
        <li key={index} className="stages__column" aria-label={`Stage ${index + 1}`}>
          {ids.map((id) => {
            const step = byId.get(id);
            return (
              <div key={id} className="stages__step">
                <span className="stages__id">{id}</span>
                <span className="muted">{step?.type === "http" ? `HTTP ${step.http?.method ?? "POST"}` : "No-op"}</span>
                {step?.http?.url && <code className="stages__url">{step.http.url}</code>}
              </div>
            );
          })}
        </li>
      ))}
    </ol>
  );
}

function TestEventForm({ workflow }: { workflow: WorkflowDetail }) {
  const ingest = useIngestEvent();
  const navigate = useNavigate();
  const [payload, setPayload] = useState('{\n  "orderId": "ord-42"\n}');
  const [problem, setProblem] = useState<string | null>(null);

  function submit(event: FormEvent) {
    event.preventDefault();
    let parsed: unknown;
    try {
      parsed = JSON.parse(payload);
    } catch {
      setProblem("The payload must be valid JSON.");
      return;
    }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      setProblem("The payload must be a JSON object.");
      return;
    }
    setProblem(null);
    ingest.mutate(
      { workflowId: workflow.id, idempotencyKey: `dashboard-${crypto.randomUUID()}`, type: workflow.definition.eventType, payload: parsed as Record<string, unknown> },
      { onSuccess: (result) => navigate(`/executions/${result.executionId}`) },
    );
  }

  return (
    <Panel title="Send a test event">
      {ingest.error && <ErrorNotice error={ingest.error} />}
      <form className="stack" onSubmit={submit}>
        <Field label={`Payload for ${workflow.definition.eventType}`} error={problem ?? undefined}>
          <textarea rows={6} value={payload} spellCheck={false} onChange={(event) => setPayload(event.target.value)} />
        </Field>
        <div>
          <Button type="submit" variant="primary" busy={ingest.isPending}>
            Send event
          </Button>
        </div>
      </form>
    </Panel>
  );
}

function VersionForm({ workflow }: { workflow: WorkflowDetail }) {
  const create = useCreateWorkflowVersion();
  const [source, setSource] = useState(() => JSON.stringify(workflow.definition, null, 2));
  const [problem, setProblem] = useState<string | null>(null);

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = parseDefinition(source);
    if (parsed.error !== undefined) {
      setProblem(parsed.error);
      return;
    }
    setProblem(null);
    create.mutate({ workflowId: workflow.id, definition: parsed.definition });
  }

  return (
    <Panel title="Draft a new version">
      <p className="muted">Saved versions are immutable. Activate one when you are ready; runs already in progress keep their version.</p>
      {create.error && <ErrorNotice error={create.error} />}
      {create.isSuccess && (
        <div className="notice notice--ok" role="status">
          <p>Saved version {create.data.version}. Activate it below to start using it.</p>
        </div>
      )}
      <form className="stack" onSubmit={submit}>
        <Field label="Definition" error={problem ?? undefined}>
          <textarea rows={12} value={source} spellCheck={false} onChange={(event) => setSource(event.target.value)} />
        </Field>
        <div>
          <Button type="submit" busy={create.isPending}>
            Save version
          </Button>
        </div>
      </form>
    </Panel>
  );
}

export function WorkflowDetailPage() {
  const { workflowId = "" } = useParams();
  const { can } = useSession();
  const workflow = useWorkflow(workflowId);
  const activate = useActivateVersion();

  if (workflow.isPending) return <Loading />;
  if (workflow.isError) return <ErrorNotice error={workflow.error} onRetry={() => void workflow.refetch()} />;
  const data = workflow.data;

  return (
    <>
      <PageHeader
        title={data.name}
        description={`Runs on ${data.definition.eventType}${data.approvalRequired ? ", after a second person approves" : ""}.`}
        actions={
          <>
            <StatusBadge status={data.status} />
            <Link to={`/executions?workflowId=${data.id}`}>View runs</Link>
          </>
        }
      />
      <Panel title={`Version ${data.version}`}>
        <StageView workflow={data} />
      </Panel>
      <Panel title="Versions">
        {activate.error && <ErrorNotice error={activate.error} />}
        <ul className="versions">
          {(data.versions ?? []).map((version) => (
            <li key={version}>
              <span>v{version}</span>
              {version === data.version ? (
                <StatusBadge status="active" />
              ) : (
                <Guard allowed={can("workflow:write")}>
                  <Button variant="quiet" busy={activate.isPending} onClick={() => activate.mutate({ workflowId: data.id, version })}>
                    Activate v{version}
                  </Button>
                </Guard>
              )}
            </li>
          ))}
        </ul>
      </Panel>
      <Guard allowed={can("event:write")}>
        <TestEventForm workflow={data} />
      </Guard>
      <Guard allowed={can("workflow:write")}>
        <VersionForm key={data.version} workflow={data} />
      </Guard>
    </>
  );
}
