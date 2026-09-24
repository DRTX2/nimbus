import { useMembers, useUpdateMember, type Role } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, EmptyState, ErrorNotice, humanize, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

const roles: Role[] = ["viewer", "operator", "approver", "owner"];
const kinds: Record<string, string> = { api_key: "API key", oidc: "Single sign-on", system: "Automation" };

export function MembersPage() {
  const { principal, can } = useSession();
  const members = useMembers();
  const update = useUpdateMember();
  const editable = can("identity:write");

  return (
    <>
      <PageHeader title="Members" description="Everyone and everything with access to this tenant. Single sign-on users join as viewers." />
      {update.error && <ErrorNotice error={update.error} />}
      <Panel>
        {members.isPending ? (
          <Loading />
        ) : members.isError ? (
          <ErrorNotice error={members.error} onRetry={() => void members.refetch()} />
        ) : members.data.length === 0 ? (
          <EmptyState title="No members" />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Signs in with</th>
                  <th scope="col">Role</th>
                  <th scope="col">Status</th>
                  <th scope="col">Since</th>
                  <th scope="col">
                    <span className="visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {members.data.map((member) => {
                  const self = member.principalId === principal?.id;
                  const locked = !editable || self || member.kind === "system";
                  return (
                    <tr key={member.id}>
                      <td>
                        {member.displayName || "Unnamed"}
                        {self && <span className="muted"> (you)</span>}
                      </td>
                      <td>{kinds[member.kind ?? ""] ?? "—"}</td>
                      <td>
                        {locked ? (
                          humanize(member.role)
                        ) : (
                          <select
                            aria-label={`Role for ${member.displayName}`}
                            value={member.role}
                            onChange={(event) => update.mutate({ id: member.id, role: event.target.value as Role, status: member.status as "active" | "revoked" })}
                          >
                            {roles.map((role) => (
                              <option key={role} value={role}>
                                {humanize(role)}
                              </option>
                            ))}
                          </select>
                        )}
                      </td>
                      <td>
                        <StatusBadge status={member.status} />
                      </td>
                      <td>
                        <Time value={member.createdAt} />
                      </td>
                      <td>
                        {!locked && (
                          <Button
                            variant={member.status === "active" ? "danger" : "secondary"}
                            onClick={() => update.mutate({ id: member.id, role: member.role as Role, status: member.status === "active" ? "revoked" : "active" })}
                          >
                            {member.status === "active" ? "Suspend" : "Restore"}
                          </Button>
                        )}
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
