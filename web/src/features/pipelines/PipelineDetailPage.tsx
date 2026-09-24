import { useState, type FormEvent } from "react";
import { useParams } from "react-router";
import { PipelineStrip } from "@/features/incidents/PipelineStrip";
import { useCheckPipeline, usePipeline, useRecordObservation, type Observation } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { formatAbsolute } from "@/shared/lib/format";
import { Button, ErrorNotice, Field, Guard, humanize, Loading, PageHeader, Panel, StatusBadge } from "@/shared/ui/ui";

type Metrics = Omit<Observation, "windowStartedAt" | "windowEndedAt" | "$schema">;

const metricFields: { key: keyof Metrics; label: string }[] = [
  { key: "visibleMessages", label: "Waiting messages" },
  { key: "inFlightMessages", label: "In-flight messages" },
  { key: "oldestMessageSeconds", label: "Oldest message (s)" },
  { key: "lambdaInvocations", label: "Invocations" },
  { key: "lambdaErrors", label: "Errors" },
  { key: "lambdaThrottles", label: "Throttles" },
  { key: "dlqMessages", label: "Dead letters" },
];

/** The most recent complete five-minute UTC window, which the API requires. */
export function lastCompleteWindow(now: Date = new Date()): { start: Date; end: Date } {
  const size = 5 * 60 * 1000;
  const end = new Date(Math.floor(now.getTime() / size) * size);
  return { start: new Date(end.getTime() - size), end };
}

function ObservationForm({ pipelineId }: { pipelineId: string }) {
  const record = useRecordObservation();
  const [metrics, setMetrics] = useState<Metrics>({
    visibleMessages: 0,
    inFlightMessages: 0,
    oldestMessageSeconds: 0,
    lambdaInvocations: 0,
    lambdaErrors: 0,
    lambdaThrottles: 0,
    dlqMessages: 0,
  });
  const window = lastCompleteWindow();

  function submit(event: FormEvent) {
    event.preventDefault();
    const { start, end } = lastCompleteWindow();
    record.mutate({ pipelineId, body: { ...metrics, windowStartedAt: start.toISOString(), windowEndedAt: end.toISOString() } });
  }

  const changed = record.data?.incidents ?? [];
  return (
    <Panel title="Submit test metrics">
      <p className="muted">
        Records the window ending {formatAbsolute(window.end.toISOString())}. Detectors open an incident after the configured number of consecutive
        breached windows.
      </p>
      {record.error && <ErrorNotice error={record.error} />}
      {record.isSuccess && (
        <div className="notice notice--ok" role="status">
          <p>
            {changed.length === 0
              ? "Window recorded. No incident changed."
              : `Window recorded. ${changed.map((incident) => `${humanize(incident.detector)} is ${incident.status}`).join("; ")}.`}
          </p>
        </div>
      )}
      <form onSubmit={submit}>
        <div className="form-grid">
          {metricFields.map((field) => (
            <Field key={field.key} label={field.label}>
              <input
                type="number"
                min={0}
                value={metrics[field.key]}
                onChange={(event) => setMetrics((current) => ({ ...current, [field.key]: Math.max(0, Number(event.target.value)) }))}
              />
            </Field>
          ))}
        </div>
        <Button type="submit" variant="primary" busy={record.isPending}>
          Record window
        </Button>
      </form>
    </Panel>
  );
}

export function PipelineDetailPage() {
  const { pipelineId = "" } = useParams();
  const { can } = useSession();
  const pipeline = usePipeline(pipelineId);
  const check = useCheckPipeline();

  if (pipeline.isPending) return <Loading />;
  if (pipeline.isError) return <ErrorNotice error={pipeline.error} onRetry={() => void pipeline.refetch()} />;
  const data = pipeline.data;

  return (
    <>
      <PageHeader
        title={data.name}
        description={`${data.mode === "aws" ? "AWS CloudWatch" : "Test data"} in ${data.region}`}
        actions={
          <>
            <StatusBadge status={data.status} />
            <Guard allowed={data.mode === "aws" && can("pipeline:write")}>
              <Button variant="primary" busy={check.isPending} onClick={() => check.mutate(data.id)}>
                Check CloudWatch now
              </Button>
            </Guard>
          </>
        }
      />
      {check.error && <ErrorNotice error={check.error} />}
      <Panel title="Watched path">
        <PipelineStrip queue={data.queueArn} lambda={data.lambdaArn} dlq={data.dlqArn} />
      </Panel>
      <Panel title="Detection settings">
        <dl className="definition-list">
          <dt>Alert after</dt>
          <dd>{data.consecutiveWindows} consecutive five-minute windows</dd>
          <dt>Backlog threshold</dt>
          <dd>{data.backlogThreshold} waiting messages</dd>
          <dt>Age threshold</dt>
          <dd>{data.ageThresholdSeconds} seconds</dd>
          {data.roleArn && (
            <>
              <dt>Role</dt>
              <dd>
                <code>{data.roleArn}</code>
              </dd>
            </>
          )}
        </dl>
      </Panel>
      <Guard allowed={data.mode === "fixture" && can("pipeline:write")}>
        <ObservationForm pipelineId={data.id} />
      </Guard>
    </>
  );
}
