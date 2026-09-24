import { cloneElement, useId, type ButtonHTMLAttributes, type ReactElement, type ReactNode } from "react";
import { ApiError } from "@/shared/api/client";
import { formatAbsolute, formatRelative } from "@/shared/lib/format";
import "./ui.css";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "danger" | "quiet";
  busy?: boolean;
};

export function Button({ variant = "secondary", busy = false, disabled, children, type = "button", ...rest }: ButtonProps) {
  return (
    <button {...rest} type={type} className={`button button--${variant}`} disabled={disabled || busy} aria-busy={busy || undefined}>
      {children}
    </button>
  );
}

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {description && <p className="page-header__description">{description}</p>}
      </div>
      {actions && <div className="page-header__actions">{actions}</div>}
    </header>
  );
}

export function Panel({ title, actions, children }: { title?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="panel" aria-label={title}>
      {(title || actions) && (
        <div className="panel__head">
          {title && <h2>{title}</h2>}
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

export type Tone = "neutral" | "info" | "ok" | "warn" | "critical";

const statusTones: Record<string, Tone> = {
  succeeded: "ok",
  recovered: "ok",
  resolved: "neutral",
  approved: "ok",
  active: "ok",
  running: "info",
  queued: "info",
  acknowledged: "warn",
  awaiting_approval: "warn",
  pending: "warn",
  warning: "warn",
  open: "critical",
  failed: "critical",
  critical: "critical",
  rejected: "neutral",
  cancelled: "neutral",
  expired: "neutral",
  revoked: "neutral",
  inactive: "neutral",
};

export function humanize(value: string): string {
  const text = value.replaceAll("_", " ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function StatusBadge({ status, tone }: { status: string; tone?: Tone }) {
  return <span className={`badge badge--${tone ?? statusTones[status] ?? "neutral"}`}>{humanize(status)}</span>;
}

export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      <h3>{title}</h3>
      {children && <p>{children}</p>}
      {action}
    </div>
  );
}

export function ErrorNotice({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof ApiError ? error.explanation : "Something unexpected happened while loading this view.";
  const hint = error instanceof ApiError && error.status === 403 ? "Your role does not include this action. Ask a tenant owner for access." : null;
  return (
    <div className="notice notice--critical" role="alert">
      <p>{message}</p>
      {hint && <p>{hint}</p>}
      {onRetry && (
        <Button variant="quiet" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}

export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="loading" role="status" aria-live="polite">
      <span className="loading__bar" aria-hidden="true" />
      <span className="visually-hidden">{label}</span>
    </div>
  );
}

export function Time({ value }: { value?: string | null }) {
  if (!value) return <span className="muted">—</span>;
  return (
    <time dateTime={value} title={formatAbsolute(value)}>
      {formatRelative(value)}
    </time>
  );
}

export function Identifier({ value }: { value: string }) {
  return (
    <code className="identifier" title={value}>
      {value.slice(0, 8)}
    </code>
  );
}

type ControlProps = { id?: string; "aria-describedby"?: string; "aria-invalid"?: boolean };

/**
 * Labels a single form control. Hints and errors are linked with
 * aria-describedby so they are announced without polluting the label.
 */
export function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string; children: ReactElement<ControlProps> }) {
  const id = useId();
  const describedBy = error ? `${id}-error` : hint ? `${id}-hint` : undefined;
  return (
    <div className="field">
      <label className="field__label" htmlFor={id}>
        {label}
      </label>
      {cloneElement(children, { id, "aria-describedby": describedBy, "aria-invalid": error ? true : undefined })}
      {hint && !error && (
        <span className="field__hint" id={`${id}-hint`}>
          {hint}
        </span>
      )}
      {error && (
        <span className="field__error" id={`${id}-error`} role="alert">
          {error}
        </span>
      )}
    </div>
  );
}

/** Shows a control only when the caller holds the permission. */
export function Guard({ allowed, children }: { allowed: boolean; children: ReactNode }) {
  return allowed ? <>{children}</> : null;
}
