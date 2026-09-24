import { useAuditEvents } from "@/shared/api/queries";
import { EmptyState, ErrorNotice, humanize, Identifier, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";

export function AuditPage() {
  const events = useAuditEvents();
  return (
    <>
      <PageHeader title="Audit log" description="The 100 most recent governed changes. Entries are append-only and never include secrets or payloads." />
      <Panel>
        {events.isPending ? (
          <Loading />
        ) : events.isError ? (
          <ErrorNotice error={events.error} onRetry={() => void events.refetch()} />
        ) : events.data.length === 0 ? (
          <EmptyState title="Nothing recorded yet" />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">When</th>
                  <th scope="col">Action</th>
                  <th scope="col">Resource</th>
                  <th scope="col">Actor</th>
                  <th scope="col">Result</th>
                </tr>
              </thead>
              <tbody>
                {events.data.map((event) => (
                  <tr key={event.id}>
                    <td>
                      <Time value={event.occurredAt} />
                    </td>
                    <td>{humanize(event.action.replaceAll(".", " "))}</td>
                    <td>
                      {humanize(event.resourceType)} <Identifier value={event.resourceId} />
                    </td>
                    <td>{event.actorId ? <Identifier value={event.actorId} /> : "—"}</td>
                    <td>
                      <StatusBadge status={event.result} />
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
