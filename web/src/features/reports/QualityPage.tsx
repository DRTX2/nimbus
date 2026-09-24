import { useState } from "react";
import { useWeeklyQuality } from "@/shared/api/queries";
import { formatPercent } from "@/shared/lib/format";
import { Button, EmptyState, ErrorNotice, humanize, Loading, PageHeader, Panel } from "@/shared/ui/ui";

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeZone: "UTC" });

export function QualityPage() {
  const [weeksAgo, setWeeksAgo] = useState(0);
  const report = useWeeklyQuality(weeksAgo);

  const range = report.data
    ? `${dateFormat.format(new Date(report.data.weekStart))} to ${dateFormat.format(new Date(new Date(report.data.weekEnd).getTime() - 1))} (UTC)`
    : "";

  return (
    <>
      <PageHeader
        title="Detection quality"
        description="How often each detector was right, based on the reviews your team recorded."
        actions={
          <div className="actions-row">
            <Button onClick={() => setWeeksAgo((value) => Math.min(52, value + 1))} disabled={weeksAgo >= 52}>
              Previous week
            </Button>
            <Button onClick={() => setWeeksAgo((value) => Math.max(0, value - 1))} disabled={weeksAgo === 0}>
              Next week
            </Button>
          </div>
        }
      />
      <Panel title={range || "Week"}>
        {report.isPending ? (
          <Loading />
        ) : report.isError ? (
          <ErrorNotice error={report.error} onRetry={() => void report.refetch()} />
        ) : (report.data.items ?? []).length === 0 ? (
          <EmptyState title="No incidents opened this week">Pick another week, or check back after incidents are reviewed.</EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">Pipeline</th>
                  <th scope="col">Detector</th>
                  <th scope="col">Incidents</th>
                  <th scope="col">Reviewed</th>
                  <th scope="col">Real problems</th>
                  <th scope="col">False alarms</th>
                  <th scope="col">False alarm rate</th>
                </tr>
              </thead>
              <tbody>
                {(report.data.items ?? []).map((row) => (
                  <tr key={`${row.pipelineId}-${row.detector}`}>
                    <td>{row.pipelineName}</td>
                    <td>{humanize(row.detector)}</td>
                    <td>{row.total}</td>
                    <td>
                      {row.reviewed} of {row.total}
                    </td>
                    <td>{row.truePositives}</td>
                    <td>{row.falsePositives}</td>
                    <td>{row.truePositives + row.falsePositives > 0 ? formatPercent(row.falsePositiveRate) : "—"}</td>
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
