import { Link, useParams } from "react-router";
import { useExecution } from "@/shared/api/queries";
import { formatAbsolute } from "@/shared/lib/format";
import { ErrorNotice, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

export function ExecutionDetailPage() {
  const { executionId = "" } = useParams();
  const execution = useExecution(executionId);

  if (execution.isPending) return <Loading />;
  if (execution.isError) return <ErrorNotice error={execution.error} onRetry={() => void execution.refetch()} />;
  const data = execution.data;

  return (
    <>
      <PageHeader
        title={`Run ${data.id.slice(0, 8)}`}
        description={`Created ${formatAbsolute(data.createdAt)} from version ${data.workflowVersion}.`}
        actions={
          <>
            <StatusBadge status={data.status} />
            <Link to={`/workflows/${data.workflowId}`}>Open workflow</Link>
          </>
        }
      />
      {data.error && (
        <div className="notice notice--critical" role="alert">
          <p>{data.error}</p>
        </div>
      )}
      {data.status === "awaiting_approval" && (
        <div className="notice notice--ok" role="status">
          <p>
            This run waits for a second person. <Link to="/approvals">Review approvals</Link>
          </p>
        </div>
      )}
      <Panel title="Steps">
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">Step</th>
                <th scope="col">Type</th>
                <th scope="col">Status</th>
                <th scope="col">Attempts</th>
                <th scope="col">Started</th>
                <th scope="col">Result</th>
              </tr>
            </thead>
            <tbody>
              {(data.steps ?? []).map((step) => (
                <tr key={step.id}>
                  <td>{step.stepId}</td>
                  <td>{step.type === "http" ? "HTTP" : "No-op"}</td>
                  <td>
                    <StatusBadge status={step.status} />
                  </td>
                  <td>{step.attempt}</td>
                  <td>
                    <Time value={step.startedAt} />
                  </td>
                  <td>{step.error ? <span className="field__error">{step.error}</span> : step.output ? <code>{JSON.stringify(step.output)}</code> : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}
