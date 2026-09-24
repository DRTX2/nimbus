import { NavLink, Outlet } from "react-router";
import { useApprovals, useIncidents } from "@/shared/api/queries";
import { useSession } from "@/shared/auth/session";
import { Button, humanize } from "@/shared/ui/ui";
import "./shell.css";

type NavItem = { to: string; label: string; permission: string; count?: number };

export function Shell() {
  const { principal, can, signOut } = useSession();
  const active = useIncidents("active");
  const approvals = useApprovals();
  const pending = approvals.data?.filter((approval) => approval.status === "pending").length ?? 0;

  const sections: { title: string; items: NavItem[] }[] = [
    {
      title: "Respond",
      items: [
        { to: "/", label: "Overview", permission: "analytics:read" },
        { to: "/incidents", label: "Incidents", permission: "incident:read", count: active.data?.length },
        { to: "/approvals", label: "Approvals", permission: "approval:read", count: pending },
      ],
    },
    {
      title: "Configure",
      items: [
        { to: "/pipelines", label: "Pipelines", permission: "pipeline:read" },
        { to: "/workflows", label: "Workflows", permission: "workflow:read" },
        { to: "/executions", label: "Executions", permission: "execution:read" },
        { to: "/reports/quality", label: "Detection quality", permission: "incident:read" },
      ],
    },
    {
      title: "Govern",
      items: [
        { to: "/access/api-keys", label: "API keys", permission: "identity:read" },
        { to: "/access/members", label: "Members", permission: "identity:read" },
        { to: "/access/audit", label: "Audit log", permission: "audit:read" },
      ],
    },
  ];

  return (
    <div className="shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <aside className="rail">
        <div className="brand">
          <svg viewBox="0 0 32 32" aria-hidden="true" className="brand__mark">
            <rect width="32" height="32" rx="7" />
            <path d="M7 20h5l3-8 3 12 3-6h4" />
          </svg>
          <span>Nimbus</span>
        </div>
        <nav aria-label="Primary">
          {sections.map((section) => {
            const visible = section.items.filter((item) => can(item.permission));
            if (visible.length === 0) return null;
            return (
              <div className="rail__section" key={section.title}>
                <p className="rail__title">{section.title}</p>
                <ul>
                  {visible.map((item) => (
                    <li key={item.to}>
                      <NavLink to={item.to} end={item.to === "/"} className="rail__link">
                        <span>{item.label}</span>
                        {item.count ? <span className="rail__count">{item.count}</span> : null}
                      </NavLink>
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
        </nav>
        {principal && (
          <div className="rail__principal">
            <p className="rail__name">{principal.name}</p>
            <p className="rail__role">{humanize(principal.role)}</p>
            <Button variant="quiet" onClick={signOut}>
              Sign out
            </Button>
          </div>
        )}
      </aside>
      <main id="main" className="content" tabIndex={-1}>
        <Outlet />
      </main>
    </div>
  );
}
