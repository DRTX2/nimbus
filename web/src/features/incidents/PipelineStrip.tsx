import { formatCount, formatDuration } from "@/shared/lib/format";
import "./pipeline-strip.css";

export type StripMetrics = {
  visibleMessages?: number;
  inFlightMessages?: number;
  oldestMessageSeconds?: number;
  dlqMessages?: number;
  lambdaInvocations?: number;
  lambdaErrors?: number;
  lambdaThrottles?: number;
};

type Stage = "queue" | "lambda" | "dlq";

/** Which stages each detector implicates, so the strip points at the fault. */
const implicated: Record<string, Stage[]> = {
  consumer_stalled: ["queue", "lambda"],
  lambda_throttled: ["lambda"],
  dlq_growth: ["dlq"],
  retry_storm: ["queue", "lambda"],
};

type Props = {
  queue: string;
  lambda: string;
  dlq: string;
  metrics?: StripMetrics;
  detector?: string;
  severity?: string;
};

function shortName(arn: string): string {
  const tail = arn.split(":").pop() ?? arn;
  return tail.split("/").pop() ?? tail;
}

/**
 * The SQS -> Lambda -> DLQ path drawn as three stages. Stages implicated by the
 * active detector are marked so responders see where the fault sits before
 * reading any numbers.
 */
export function PipelineStrip({ queue, lambda, dlq, metrics, detector, severity = "critical" }: Props) {
  const hot = new Set(detector ? implicated[detector] ?? [] : []);
  const stage = (id: Stage, title: string, resource: string, facts: [string, string][]) => (
    <li className={`strip__stage${hot.has(id) ? ` strip__stage--${severity === "warning" ? "warn" : "critical"}` : ""}`} aria-label={`${title}${hot.has(id) ? ", implicated" : ""}`}>
      <span className="strip__kind">{title}</span>
      <span className="strip__resource" title={resource}>
        {shortName(resource)}
      </span>
      {metrics && (
        <dl className="strip__facts">
          {facts.map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      )}
    </li>
  );
  const m = metrics ?? {};
  return (
    <ol className="strip">
      {stage("queue", "Queue", queue, [
        ["Waiting", formatCount(m.visibleMessages ?? 0)],
        ["In flight", formatCount(m.inFlightMessages ?? 0)],
        ["Oldest", formatDuration(m.oldestMessageSeconds ?? 0)],
      ])}
      {stage("lambda", "Consumer", lambda, [
        ["Invocations", formatCount(m.lambdaInvocations ?? 0)],
        ["Errors", formatCount(m.lambdaErrors ?? 0)],
        ["Throttles", formatCount(m.lambdaThrottles ?? 0)],
      ])}
      {stage("dlq", "Dead letters", dlq, [["Messages", formatCount(m.dlqMessages ?? 0)]])}
    </ol>
  );
}
