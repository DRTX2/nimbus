import { useState, type FormEvent } from "react";
import { useApiKeys, useCreateApiKey, useRevokeApiKey, type Role } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, EmptyState, ErrorNotice, Field, Guard, humanize, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

const roles: { value: Role; description: string }[] = [
  { value: "viewer", description: "Read everything except access settings" },
  { value: "operator", description: "Manage workflows, pipelines and incidents" },
  { value: "approver", description: "Approve or reject protected runs" },
  { value: "owner", description: "Everything, including access" },
];

function CreateKeyForm() {
  const create = useCreateApiKey();
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [days, setDays] = useState(90);
  const [problem, setProblem] = useState<string | null>(null);

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) {
      setProblem("Name the key after the person or system that will use it.");
      return;
    }
    setProblem(null);
    create.mutate({ name: name.trim(), role, expiresInDays: days }, { onSuccess: () => setName("") });
  }

  return (
    <Panel title="Issue an API key">
      {create.error && <ErrorNotice error={create.error} />}
      {create.data && (
        <div className="notice notice--ok secret" role="status">
          <p>Copy this key now. Nimbus stores only a hash and cannot show it again.</p>
          <code data-testid="issued-token">{create.data.token}</code>
        </div>
      )}
      <form onSubmit={submit} noValidate>
        <div className="form-grid">
          <Field label="Name" error={problem ?? undefined}>
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="CI deploy bot" />
          </Field>
          <Field label="Role" hint={roles.find((option) => option.value === role)?.description}>
            <select value={role} onChange={(event) => setRole(event.target.value as Role)}>
              {roles.map((option) => (
                <option key={option.value} value={option.value}>
                  {humanize(option.value)}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Expires after (days)">
            <input type="number" min={1} max={365} value={days} onChange={(event) => setDays(Math.min(365, Math.max(1, Number(event.target.value))))} />
          </Field>
        </div>
        <Button type="submit" variant="primary" busy={create.isPending}>
          Issue key
        </Button>
      </form>
    </Panel>
  );
}

export function ApiKeysPage() {
  const { can } = useSession();
  const keys = useApiKeys();
  const revoke = useRevokeApiKey();

  return (
    <>
      <PageHeader title="API keys" description="Credentials for people and systems. Each key carries one role and can be revoked at any time." />
      <Guard allowed={can("identity:write")}>
        <CreateKeyForm />
      </Guard>
      {revoke.error && <ErrorNotice error={revoke.error} />}
      <Panel>
        {keys.isPending ? (
          <Loading />
        ) : keys.isError ? (
          <ErrorNotice error={keys.error} onRetry={() => void keys.refetch()} />
        ) : keys.data.length === 0 ? (
          <EmptyState title="No API keys" />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Prefix</th>
                  <th scope="col">Role</th>
                  <th scope="col">Expires</th>
                  <th scope="col">Status</th>
                  <th scope="col">
                    <span className="visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {keys.data.map((key) => (
                  <tr key={key.id}>
                    <td>{key.name}</td>
                    <td>
                      <code>{key.prefix}…</code>
                    </td>
                    <td>{humanize(key.role)}</td>
                    <td>{key.expiresAt ? <Time value={key.expiresAt} /> : "Never"}</td>
                    <td>
                      <StatusBadge status={key.revokedAt ? "revoked" : "active"} />
                    </td>
                    <td>
                      {!key.revokedAt && can("identity:write") && (
                        <Button variant="danger" busy={revoke.isPending && revoke.variables === key.id} onClick={() => revoke.mutate(key.id)}>
                          Revoke
                        </Button>
                      )}
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
