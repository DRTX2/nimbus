import { Navigate, Outlet, useLocation, useRouteError, type RouteObject } from "react-router";
import { SignInPage } from "@/features/auth/SignInPage";
import { ApprovalsPage } from "@/features/approvals/ApprovalsPage";
import { ExecutionDetailPage } from "@/features/executions/ExecutionDetailPage";
import { ExecutionsPage } from "@/features/executions/ExecutionsPage";
import { ApiKeysPage } from "@/features/governance/ApiKeysPage";
import { AuditPage } from "@/features/governance/AuditPage";
import { MembersPage } from "@/features/governance/MembersPage";
import { IncidentDetailPage } from "@/features/incidents/IncidentDetailPage";
import { IncidentsPage } from "@/features/incidents/IncidentsPage";
import { OverviewPage } from "@/features/overview/OverviewPage";
import { PipelineDetailPage } from "@/features/pipelines/PipelineDetailPage";
import { PipelinesPage } from "@/features/pipelines/PipelinesPage";
import { QualityPage } from "@/features/reports/QualityPage";
import { WorkflowDetailPage } from "@/features/workflows/WorkflowDetailPage";
import { WorkflowsPage } from "@/features/workflows/WorkflowsPage";
import { useSession } from "@/shared/auth/session";
import { EmptyState, ErrorNotice, Loading } from "@/shared/ui/ui";
import { Shell } from "./Shell";

function RequireSession() {
  const { status } = useSession();
  const location = useLocation();
  if (status === "loading") return <Loading label="Restoring your session" />;
  if (status === "anonymous") return <Navigate to="/sign-in" replace state={{ from: location.pathname }} />;
  return <Outlet />;
}

function RouteError() {
  return <ErrorNotice error={useRouteError()} />;
}

function NotFound() {
  return <EmptyState title="This page does not exist">Check the address, or pick a section from the navigation.</EmptyState>;
}

export const routes: RouteObject[] = [
  { path: "/sign-in", element: <SignInPage /> },
  {
    element: <RequireSession />,
    errorElement: <RouteError />,
    children: [
      {
        element: <Shell />,
        children: [
          { index: true, element: <OverviewPage /> },
          { path: "incidents", element: <IncidentsPage /> },
          { path: "incidents/:incidentId", element: <IncidentDetailPage /> },
          { path: "approvals", element: <ApprovalsPage /> },
          { path: "pipelines", element: <PipelinesPage /> },
          { path: "pipelines/:pipelineId", element: <PipelineDetailPage /> },
          { path: "workflows", element: <WorkflowsPage /> },
          { path: "workflows/:workflowId", element: <WorkflowDetailPage /> },
          { path: "executions", element: <ExecutionsPage /> },
          { path: "executions/:executionId", element: <ExecutionDetailPage /> },
          { path: "reports/quality", element: <QualityPage /> },
          { path: "access/api-keys", element: <ApiKeysPage /> },
          { path: "access/members", element: <MembersPage /> },
          { path: "access/audit", element: <AuditPage /> },
          { path: "*", element: <NotFound /> },
        ],
      },
    ],
  },
];
