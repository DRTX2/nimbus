import { Link, useSearchParams } from "react-router";
import { useExecutions, type ExecutionStatus } from "@/shared/api/queries";
import { Button, EmptyState, ErrorNotice, Field, Identifier, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

const statuses: { value: ExecutionStatus | ""; label: string }[] = [
  { value: "", label: "Any status" },
  { value: "running", label: "Running" },
  { value: "queued", label: "Queued" },
  { value: "awaiting_approval", label: "Awaiting approval" },
  { value: "succeeded", label: "Succeeded" },
  { value: "failed", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
];

export function ExecutionsPage() {
  const [params, setParams] = useSearchParams();
  const status = (params.get("status") ?? "") as ExecutionStatus | "";
  const workflowId = params.get("workflowId") ?? "";
  const executions = useExecutions({ ...(status ? { status } : {}), ...(workflowId ? { workflowId } : {}) });
  const items = executions.data?.pages.flatMap((page) => page.items ?? []) ?? [];

  function update(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next);
  }

  return (
    <>
      <PageHeader title="Executions" description="Every workflow run with its steps, attempts and outcome." />
      <div className="form-grid">
        <Field label="Status">
          <select value={status} onChange={(event) => update("status", event.target.value)}>
            {statuses.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </Field>
        {workflowId && (
          <div className="actions-row">
            <span className="muted">Filtered to one workflow.</span>
            <Button variant="quiet" onClick={() => update("workflowId", "")}>
              Show all workflows
            </Button>
          </div>
        )}
      </div>
      <Panel>
        {executions.isPending ? (
          <Loading />
        ) : executions.isError ? (
          <ErrorNotice error={executions.error} onRetry={() => void executions.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState title="No runs match">Runs appear here as soon as a workflow accepts an event.</EmptyState>
        ) : (
          <>
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th scope="col">Run</th>
                    <th scope="col">Workflow</th>
                    <th scope="col">Status</th>
                    <th scope="col">Started</th>
                    <th scope="col">Finished</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((execution) => (
                    <tr key={execution.id}>
                      <td>
                        <Link to={`/executions/${execution.id}`}>
                          <Identifier value={execution.id} />
                        </Link>
                      </td>
                      <td>
                        <Link to={`/workflows/${execution.workflowId}`}>{execution.workflowName}</Link>
                      </td>
                      <td>
                        <StatusBadge status={execution.status} />
                      </td>
                      <td>
                        <Time value={execution.startedAt ?? execution.createdAt} />
                      </td>
                      <td>
                        <Time value={execution.finishedAt} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {executions.hasNextPage && (
              <div className="load-more">
                <Button busy={executions.isFetchingNextPage} onClick={() => void executions.fetchNextPage()}>
                  Load older runs
                </Button>
              </div>
            )}
          </>
        )}
      </Panel>
    </>
  );
}
