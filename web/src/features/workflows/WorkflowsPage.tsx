import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router";
import { useCreateWorkflow, useWorkflows } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, EmptyState, ErrorNotice, Field, Guard, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";
import { definitionTemplate, parseDefinition } from "./definition";

function CreateWorkflowForm({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState("");
  const [source, setSource] = useState(definitionTemplate);
  const [approvalRequired, setApprovalRequired] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const create = useCreateWorkflow();
  const navigate = useNavigate();

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) {
      setProblem("Give the workflow a name.");
      return;
    }
    const parsed = parseDefinition(source);
    if (parsed.error !== undefined) {
      setProblem(parsed.error);
      return;
    }
    setProblem(null);
    create.mutate(
      { name: name.trim(), approvalRequired, definition: parsed.definition },
      {
        onSuccess: (workflow) => {
          onDone();
          navigate(`/workflows/${workflow.id}`);
        },
      },
    );
  }

  return (
    <Panel title="Create a workflow">
      {create.error && <ErrorNotice error={create.error} />}
      <form className="stack" onSubmit={submit} noValidate>
        <Field label="Name">
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Order notification" />
        </Field>
        <Field label="Definition" hint="JSON. Steps without dependencies start together; HTTP steps retry timeouts, 408, 429 and 5xx." error={problem ?? undefined}>
          <textarea rows={16} value={source} spellCheck={false} onChange={(event) => setSource(event.target.value)} />
        </Field>
        <label className="choice">
          <input type="checkbox" checked={approvalRequired} onChange={(event) => setApprovalRequired(event.target.checked)} />
          Require a second person to approve each run
        </label>
        <div className="actions-row">
          <Button type="submit" variant="primary" busy={create.isPending}>
            Create workflow
          </Button>
          <Button variant="quiet" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}

export function WorkflowsPage() {
  const { can } = useSession();
  const workflows = useWorkflows();
  const [creating, setCreating] = useState(false);

  return (
    <>
      <PageHeader
        title="Workflows"
        description="Versioned automations that run when an event arrives, with retries and an audit trail."
        actions={
          <Guard allowed={can("workflow:write") && !creating}>
            <Button variant="primary" onClick={() => setCreating(true)}>
              Create workflow
            </Button>
          </Guard>
        }
      />
      {creating && <CreateWorkflowForm onDone={() => setCreating(false)} />}
      <Panel>
        {workflows.isPending ? (
          <Loading />
        ) : workflows.isError ? (
          <ErrorNotice error={workflows.error} onRetry={() => void workflows.refetch()} />
        ) : workflows.data.length === 0 ? (
          <EmptyState title="No workflows yet">Create one to page on-call, call a webhook, or record an audit trail when an event arrives.</EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Starts on</th>
                  <th scope="col">Steps</th>
                  <th scope="col">Version</th>
                  <th scope="col">Status</th>
                  <th scope="col">Updated</th>
                </tr>
              </thead>
              <tbody>
                {workflows.data.map((workflow) => (
                  <tr key={workflow.id}>
                    <td>
                      <Link to={`/workflows/${workflow.id}`}>{workflow.name}</Link>
                      {workflow.approvalRequired && <span className="muted"> (needs approval)</span>}
                    </td>
                    <td>
                      <code>{workflow.definition.eventType}</code>
                    </td>
                    <td>{workflow.definition.steps?.length ?? 0}</td>
                    <td>v{workflow.version}</td>
                    <td>
                      <StatusBadge status={workflow.status} />
                    </td>
                    <td>
                      <Time value={workflow.updatedAt} />
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
