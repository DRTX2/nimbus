import { Link, useSearchParams } from "react-router";
import { useIncidents, type Incident, type IncidentStatusFilter } from "@/shared/api/queries";
import { EmptyState, ErrorNotice, humanize, Loading, PageHeader, Panel, StatusBadge, Time } from "@/shared/ui/ui";
import "./incidents.css";

const filters: { value: IncidentStatusFilter | "all"; label: string }[] = [
  { value: "active", label: "Needs attention" },
  { value: "recovered", label: "Recovered" },
  { value: "resolved", label: "Resolved" },
  { value: "all", label: "All" },
];

export function IncidentTable({ incidents }: { incidents: Incident[] }) {
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th scope="col">Incident</th>
            <th scope="col">Pipeline</th>
            <th scope="col">Severity</th>
            <th scope="col">Status</th>
            <th scope="col">Review</th>
            <th scope="col">Opened</th>
          </tr>
        </thead>
        <tbody>
          {incidents.map((incident) => (
            <tr key={incident.id}>
              <td>
                <Link to={`/incidents/${incident.id}`}>{humanize(incident.detector)}</Link>
              </td>
              <td>{incident.pipelineName ?? "—"}</td>
              <td>
                <StatusBadge status={incident.severity} />
              </td>
              <td>
                <StatusBadge status={incident.status} />
              </td>
              <td>{incident.review ? humanize(incident.review.disposition) : <span className="muted">Not reviewed</span>}</td>
              <td>
                <Time value={incident.openedAt} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function IncidentsPage() {
  const [params, setParams] = useSearchParams();
  const selected = (params.get("status") ?? "active") as IncidentStatusFilter | "all";
  const incidents = useIncidents(selected === "all" ? undefined : selected);

  return (
    <>
      <PageHeader title="Incidents" description="Sustained pipeline symptoms, deduplicated per detector and kept with their evidence." />
      <div className="segmented" role="tablist" aria-label="Filter incidents">
        {filters.map((filter) => (
          <button
            key={filter.value}
            role="tab"
            aria-selected={selected === filter.value}
            className="segmented__option"
            onClick={() => setParams(filter.value === "active" ? {} : { status: filter.value })}
          >
            {filter.label}
          </button>
        ))}
      </div>
      <Panel>
        {incidents.isPending ? (
          <Loading />
        ) : incidents.isError ? (
          <ErrorNotice error={incidents.error} onRetry={() => void incidents.refetch()} />
        ) : incidents.data.length === 0 ? (
          <EmptyState title={selected === "active" ? "Nothing needs attention" : "No incidents here"}>
            {selected === "active" ? "Every monitored pipeline is within its thresholds." : "Incidents appear here as they change state."}
          </EmptyState>
        ) : (
          <IncidentTable incidents={incidents.data} />
        )}
      </Panel>
    </>
  );
}
