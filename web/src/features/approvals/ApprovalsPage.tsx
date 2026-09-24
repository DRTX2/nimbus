import { Link } from "react-router";
import { useApprovals, useDecideApproval } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, EmptyState, ErrorNotice, Identifier, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

export function ApprovalsPage() {
  const { principal, can } = useSession();
  const approvals = useApprovals();
  const decide = useDecideApproval();

  return (
    <>
      <PageHeader title="Approvals" description="Protected workflows wait here until someone other than the requester approves them." />
      {decide.error && <ErrorNotice error={decide.error} />}
      <Panel>
        {approvals.isPending ? (
          <Loading />
        ) : approvals.isError ? (
          <ErrorNotice error={approvals.error} onRetry={() => void approvals.refetch()} />
        ) : approvals.data.length === 0 ? (
          <EmptyState title="No approval requests">Runs of workflows that require approval will appear here.</EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">Execution</th>
                  <th scope="col">Status</th>
                  <th scope="col">Requested</th>
                  <th scope="col">Expires</th>
                  <th scope="col">
                    <span className="visually-hidden">Decision</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {approvals.data.map((approval) => {
                  const ownRequest = approval.requestedBy === principal?.id;
                  const pending = approval.status === "pending";
                  return (
                    <tr key={approval.id}>
                      <td>
                        <Link to={`/executions/${approval.executionId}`}>
                          Run <Identifier value={approval.executionId} />
                        </Link>
                      </td>
                      <td>
                        <StatusBadge status={approval.status} />
                      </td>
                      <td>
                        <Time value={approval.createdAt} />
                      </td>
                      <td>{pending ? <Time value={approval.expiresAt} /> : <span className="muted">—</span>}</td>
                      <td>
                        {pending && can("approval:decide") ? (
                          ownRequest ? (
                            <span className="muted">You requested this run</span>
                          ) : (
                            <div className="actions-row">
                              <Button variant="primary" busy={decide.isPending} onClick={() => decide.mutate({ id: approval.id, decision: "approved" })}>
                                Approve
                              </Button>
                              <Button variant="danger" busy={decide.isPending} onClick={() => decide.mutate({ id: approval.id, decision: "rejected" })}>
                                Reject
                              </Button>
                            </div>
                          )
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </>
  );
}
