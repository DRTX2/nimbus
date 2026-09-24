import { useState } from "react";
import { Link, useParams } from "react-router";
import { useAcknowledgeIncident, useIncident, usePipeline, useResolveIncident, useReviewIncident } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { formatAbsolute } from "@/shared/lib/format";
import { Button, ErrorNotice, Field, Guard, humanize, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";
import { PipelineStrip, type StripMetrics } from "./PipelineStrip";
import "./incidents.css";

const dispositions = [
  { value: "true_positive", label: "Real problem" },
  { value: "false_positive", label: "False alarm" },
  { value: "expected_behavior", label: "Expected behavior" },
  { value: "duplicate", label: "Duplicate" },
] as const;

type Disposition = (typeof dispositions)[number]["value"];

export function IncidentDetailPage() {
  const { incidentId = "" } = useParams();
  const { can } = useSession();
  const incident = useIncident(incidentId);
  const pipeline = usePipeline(incident.data?.pipelineId ?? "");
  const acknowledge = useAcknowledgeIncident();
  const review = useReviewIncident();
  const resolve = useResolveIncident();
  const [disposition, setDisposition] = useState<Disposition>("true_positive");
  const [comment, setComment] = useState("");
  const [note, setNote] = useState("");

  if (incident.isPending) return <Loading />;
  if (incident.isError) return <ErrorNotice error={incident.error} onRetry={() => void incident.refetch()} />;
  const data = incident.data;
  const evidence = (data.evidence ?? {}) as StripMetrics & { windowStartedAt?: string; windowEndedAt?: string };
  const mutationError = acknowledge.error ?? review.error ?? resolve.error;
  const closed = data.status === "resolved";

  return (
    <>
      <PageHeader
        title={humanize(data.detector)}
        description={`${data.pipelineName ?? "Pipeline"} · opened ${formatAbsolute(data.openedAt)}`}
        actions={
          <>
            <StatusBadge status={data.severity} />
            <StatusBadge status={data.status} />
          </>
        }
      />
      <p className="incident-summary">{data.summary}</p>
      {mutationError && <ErrorNotice error={mutationError} />}

      <Panel title="Where it is failing">
        {pipeline.data ? (
          <PipelineStrip
            queue={pipeline.data.queueArn}
            lambda={pipeline.data.lambdaArn}
            dlq={pipeline.data.dlqArn}
            metrics={evidence}
            detector={data.detector}
            severity={data.severity}
          />
        ) : (
          <Loading label="Loading pipeline" />
        )}
        {evidence.windowStartedAt && evidence.windowEndedAt && (
          <p className="footnote">
            Evidence from the window {formatAbsolute(evidence.windowStartedAt)} to {formatAbsolute(evidence.windowEndedAt)}.{" "}
            <Link to={`/pipelines/${data.pipelineId}`}>View pipeline</Link>
          </p>
        )}
      </Panel>

      <div className="detail-grid">
        <div className="stack">
          <Guard allowed={!closed && (can("incident:ack") || can("incident:resolve"))}>
            <Panel title="Respond">
              <div className="stack">
                <Guard allowed={can("incident:ack") && data.status === "open"}>
                  <div className="actions-row">
                    <Button variant="primary" busy={acknowledge.isPending} onClick={() => acknowledge.mutate(data.id)}>
                      Acknowledge
                    </Button>
                    <span className="muted">Tells the team someone is on it.</span>
                  </div>
                </Guard>
                <Guard allowed={can("incident:resolve")}>
                  <form
                    className="stack"
                    onSubmit={(event) => {
                      event.preventDefault();
                      resolve.mutate({ id: data.id, note: note.trim() || undefined });
                    }}
                  >
                    <Field label="Resolution note" hint="Optional. What fixed it, for the next responder.">
                      <input value={note} maxLength={500} onChange={(event) => setNote(event.target.value)} />
                    </Field>
                    <div>
                      <Button type="submit" busy={resolve.isPending}>
                        Resolve incident
                      </Button>
                    </div>
                  </form>
                </Guard>
              </div>
            </Panel>
          </Guard>

          <Panel title="Review">
            {data.review && (
              <p className="review-line">
                Classified as <strong>{dispositions.find((option) => option.value === data.review?.disposition)?.label ?? humanize(data.review.disposition)}</strong> <Time value={data.review.reviewedAt} />
                {data.review.comment ? `: “${data.review.comment}”` : "."}
              </p>
            )}
            {can("incident:classify") ? (
              <form
                className="stack"
                onSubmit={(event) => {
                  event.preventDefault();
                  review.mutate({ id: data.id, disposition, comment: comment.trim() || undefined });
                }}
              >
                <fieldset className="choice-group">
                  <legend className="field__label">Was this alert useful?</legend>
                  {dispositions.map((option) => (
                    <label key={option.value} className="choice">
                      <input type="radio" name="disposition" value={option.value} checked={disposition === option.value} onChange={() => setDisposition(option.value)} />
                      {option.label}
                    </label>
                  ))}
                </fieldset>
                <Field label="Comment" hint="Optional, up to 500 characters.">
                  <input value={comment} maxLength={500} onChange={(event) => setComment(event.target.value)} />
                </Field>
                <div>
                  <Button type="submit" busy={review.isPending}>
                    {data.review ? "Reclassify" : "Save review"}
                  </Button>
                </div>
              </form>
            ) : (
              !data.review && <p className="muted">Not reviewed yet.</p>
            )}
          </Panel>
        </div>

        <Panel title="Timeline">
          <ol className="timeline">
            {(data.timeline ?? []).map((event) => (
              <li key={event.id} data-type={event.type}>
                <p className="timeline__type">{humanize(event.type)}</p>
                <p className="timeline__meta">
                  <Time value={event.createdAt} />
                </p>
              </li>
            ))}
          </ol>
        </Panel>
      </div>
    </>
  );
}
