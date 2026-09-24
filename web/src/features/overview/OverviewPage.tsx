import { Link } from "react-router";
import { IncidentTable } from "@/features/incidents/IncidentsPage";
import { useAnalytics, useIncidents } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { formatCount, formatDuration, formatPercent } from "@/shared/lib/format";
import { EmptyState, ErrorNotice, Loading, PageHeader, Panel } from "@/shared/ui/ui";

export function OverviewPage() {
  const { principal, can } = useSession();
  const analytics = useAnalytics();
  const incidents = useIncidents("active");

  return (
    <>
      <PageHeader title={`Hello, ${principal?.name ?? "there"}`} description="What changed in your pipelines and automations over the last 24 hours." />

      <Panel title="Needs attention" actions={<Link to="/incidents">All incidents</Link>}>
        {incidents.isPending ? (
          <Loading />
        ) : incidents.isError ? (
          <ErrorNotice error={incidents.error} onRetry={() => void incidents.refetch()} />
        ) : incidents.data.length === 0 ? (
          <EmptyState title="All pipelines are healthy">
            No open incidents. {can("pipeline:write") ? <Link to="/pipelines">Add a pipeline</Link> : null}
          </EmptyState>
        ) : (
          <IncidentTable incidents={incidents.data} />
        )}
      </Panel>

      {analytics.isError ? (
        <ErrorNotice error={analytics.error} />
      ) : analytics.data ? (
        <section aria-label="Automation health" className="stats">
          <div className="stat">
            <p className="stat__value">{formatCount(analytics.data.events24h)}</p>
            <p className="stat__label">Events accepted</p>
          </div>
          <div className="stat">
            <p className="stat__value">{formatCount(analytics.data.executions24h)}</p>
            <p className="stat__label">Workflow runs</p>
          </div>
          <div className="stat">
            <p className="stat__value">{analytics.data.succeeded24h + analytics.data.failed24h > 0 ? formatPercent(analytics.data.successRate) : "—"}</p>
            <p className="stat__label">Finished successfully</p>
          </div>
          <div className="stat">
            <p className="stat__value">{formatCount(analytics.data.queuedSteps)}</p>
            <p className="stat__label">
              Steps waiting{analytics.data.queuedSteps > 0 ? `, oldest ${formatDuration(analytics.data.oldestQueuedSeconds)}` : ""}
            </p>
          </div>
        </section>
      ) : (
        <Loading />
      )}
    </>
  );
}
