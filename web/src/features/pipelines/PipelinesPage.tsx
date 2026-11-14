import { useState, type ComponentProps, type FormEvent } from "react";
import { Link, useNavigate } from "react-router";
import { useCreatePipeline, usePipelines, useWorkflows, type Workflow } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, EmptyState, ErrorNotice, Field, Guard, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

type Draft = {
  name: string;
  mode: "fixture" | "aws";
  region: string;
  queueArn: string;
  lambdaArn: string;
  dlqArn: string;
  roleArn: string;
  externalIdRef: string;
  incidentWorkflowId: string;
  recoveryWorkflowId: string;
  consecutiveWindows: number;
  backlogThreshold: number;
  ageThresholdSeconds: number;
};

const emptyDraft: Draft = {
  name: "",
  mode: "fixture",
  region: "us-east-1",
  queueArn: "",
  lambdaArn: "",
  dlqArn: "",
  roleArn: "",
  externalIdRef: "",
  incidentWorkflowId: "",
  recoveryWorkflowId: "",
  consecutiveWindows: 2,
  backlogThreshold: 100,
  ageThresholdSeconds: 300,
};

/** Client-side checks mirror the API so people fix mistakes before submitting. */
export function validatePipelineDraft(draft: Draft): Partial<Record<keyof Draft, string>> {
  const errors: Partial<Record<keyof Draft, string>> = {};
  if (!draft.name.trim()) errors.name = "Give the pipeline a name your team recognizes.";
  for (const key of ["queueArn", "lambdaArn", "dlqArn"] as const) {
    if (!draft[key].trim().startsWith("arn:")) errors[key] = "Enter the full ARN, starting with arn:";
  }
  if (draft.mode === "aws") {
    if (!draft.roleArn.startsWith("arn:aws:iam::")) errors.roleArn = "AWS pipelines need a read-only IAM role ARN.";
    if (!/^aws-external-id\/[a-z0-9][a-z0-9._-]{0,63}$/.test(draft.externalIdRef)) {
      errors.externalIdRef = "Use a configured reference such as aws-external-id/acme-prod.";
    }
  }
  if (draft.consecutiveWindows < 1 || draft.consecutiveWindows > 10) errors.consecutiveWindows = "Choose between 1 and 10 windows.";
  return errors;
}

/** Offers only workflows whose trigger matches, so dispatch can never be rejected. */
function WorkflowSelect({ eventType, workflows, value, onChange, ...control }: { eventType: string; workflows: Workflow[] | undefined; value: string; onChange: (id: string) => void } & Omit<ComponentProps<"select">, "value" | "onChange">) {
  return (
    <select {...control} value={value} onChange={(event) => onChange(event.target.value)}>
      <option value="">None</option>
      {(workflows ?? [])
        .filter((workflow) => workflow.definition.eventType === eventType)
        .map((workflow) => (
          <option key={workflow.id} value={workflow.id}>
            {workflow.name}
          </option>
        ))}
    </select>
  );
}

function CreatePipelineForm({ onDone }: { onDone: () => void }) {
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [errors, setErrors] = useState<ReturnType<typeof validatePipelineDraft>>({});
  const workflows = useWorkflows();
  const create = useCreatePipeline();
  const navigate = useNavigate();
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((current) => ({ ...current, [key]: value }));

  function submit(event: FormEvent) {
    event.preventDefault();
    const found = validatePipelineDraft(draft);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    const aws = draft.mode === "aws";
    create.mutate(
      {
        name: draft.name.trim(),
        mode: draft.mode,
        region: draft.region.trim(),
        queueArn: draft.queueArn.trim(),
        lambdaArn: draft.lambdaArn.trim(),
        dlqArn: draft.dlqArn.trim(),
        roleArn: aws ? draft.roleArn.trim() : undefined,
        externalIdRef: aws ? draft.externalIdRef.trim() : undefined,
        incidentWorkflowId: draft.incidentWorkflowId || undefined,
        recoveryWorkflowId: draft.recoveryWorkflowId || undefined,
        consecutiveWindows: draft.consecutiveWindows,
        backlogThreshold: draft.backlogThreshold,
        ageThresholdSeconds: draft.ageThresholdSeconds,
      },
      {
        onSuccess: (pipeline) => {
          onDone();
          navigate(`/pipelines/${pipeline.id}`);
        },
      },
    );
  }

  return (
    <Panel title="Add a pipeline">
      {create.error && <ErrorNotice error={create.error} />}
      <form onSubmit={submit} noValidate>
        <div className="form-grid">
          <Field label="Name" error={errors.name}>
            <input value={draft.name} onChange={(event) => set("name", event.target.value)} placeholder="Billing events" />
          </Field>
          <Field label="Source" hint="Test pipelines accept metrics you submit by hand.">
            <select value={draft.mode} onChange={(event) => set("mode", event.target.value as Draft["mode"])}>
              <option value="fixture">Test data</option>
              <option value="aws">AWS CloudWatch</option>
            </select>
          </Field>
          <Field label="Region">
            <input value={draft.region} onChange={(event) => set("region", event.target.value)} />
          </Field>
        </div>
        <div className="form-grid">
          <Field label="Queue ARN" error={errors.queueArn}>
            <input value={draft.queueArn} onChange={(event) => set("queueArn", event.target.value)} placeholder="arn:aws:sqs:us-east-1:123456789012:billing" />
          </Field>
          <Field label="Consumer function ARN" error={errors.lambdaArn}>
            <input value={draft.lambdaArn} onChange={(event) => set("lambdaArn", event.target.value)} placeholder="arn:aws:lambda:us-east-1:123456789012:function:billing" />
          </Field>
          <Field label="Dead-letter queue ARN" error={errors.dlqArn}>
            <input value={draft.dlqArn} onChange={(event) => set("dlqArn", event.target.value)} placeholder="arn:aws:sqs:us-east-1:123456789012:billing-dlq" />
          </Field>
        </div>
        {draft.mode === "aws" && (
          <div className="form-grid">
            <Field label="Read-only role ARN" error={errors.roleArn}>
              <input value={draft.roleArn} onChange={(event) => set("roleArn", event.target.value)} />
            </Field>
            <Field label="External ID reference" hint="Configured by your Nimbus operator; the secret itself never leaves the server." error={errors.externalIdRef}>
              <input value={draft.externalIdRef} onChange={(event) => set("externalIdRef", event.target.value)} placeholder="aws-external-id/acme-prod" />
            </Field>
          </div>
        )}
        <div className="form-grid">
          <Field label="Windows before alerting" hint="Consecutive five-minute windows." error={errors.consecutiveWindows}>
            <input type="number" min={1} max={10} value={draft.consecutiveWindows} onChange={(event) => set("consecutiveWindows", Number(event.target.value))} />
          </Field>
          <Field label="Backlog threshold" hint="Waiting messages.">
            <input type="number" min={1} value={draft.backlogThreshold} onChange={(event) => set("backlogThreshold", Number(event.target.value))} />
          </Field>
          <Field label="Age threshold" hint="Seconds the oldest message may wait.">
            <input type="number" min={1} value={draft.ageThresholdSeconds} onChange={(event) => set("ageThresholdSeconds", Number(event.target.value))} />
          </Field>
          <Field label="Response workflow" hint="Runs when an incident opens.">
            <WorkflowSelect eventType="nimbus.incident.opened" workflows={workflows.data} value={draft.incidentWorkflowId} onChange={(id) => set("incidentWorkflowId", id)} />
          </Field>
          <Field label="Recovery workflow" hint="Runs when an incident recovers.">
            <WorkflowSelect eventType="nimbus.incident.recovered" workflows={workflows.data} value={draft.recoveryWorkflowId} onChange={(id) => set("recoveryWorkflowId", id)} />
          </Field>
        </div>
        <div className="actions-row">
          <Button type="submit" variant="primary" busy={create.isPending}>
            Add pipeline
          </Button>
          <Button variant="quiet" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}

export function PipelinesPage() {
  const { can } = useSession();
  const pipelines = usePipelines();
  const [creating, setCreating] = useState(false);

  return (
    <>
      <PageHeader
        title="Pipelines"
        description="Queue, consumer and dead-letter queue triples that Nimbus watches without reading message contents."
        actions={
          <Guard allowed={can("pipeline:write") && !creating}>
            <Button variant="primary" onClick={() => setCreating(true)}>
              Add pipeline
            </Button>
          </Guard>
        }
      />
      {creating && <CreatePipelineForm onDone={() => setCreating(false)} />}
      <Panel>
        {pipelines.isPending ? (
          <Loading />
        ) : pipelines.isError ? (
          <ErrorNotice error={pipelines.error} onRetry={() => void pipelines.refetch()} />
        ) : pipelines.data.length === 0 ? (
          <EmptyState title="No pipelines yet">Add a pipeline to start detecting stalled consumers, throttling and dead letters.</EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Source</th>
                  <th scope="col">Region</th>
                  <th scope="col">Alerts after</th>
                  <th scope="col">Status</th>
                  <th scope="col">Added</th>
                </tr>
              </thead>
              <tbody>
                {pipelines.data.map((pipeline) => (
                  <tr key={pipeline.id}>
                    <td>
                      <Link to={`/pipelines/${pipeline.id}`}>{pipeline.name}</Link>
                    </td>
                    <td>{pipeline.mode === "aws" ? "AWS CloudWatch" : "Test data"}</td>
                    <td>{pipeline.region}</td>
                    <td>{pipeline.consecutiveWindows * 5} minutes</td>
                    <td>
                      <StatusBadge status={pipeline.status} />
                    </td>
                    <td>
                      <Time value={pipeline.createdAt} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </>
  );
}
