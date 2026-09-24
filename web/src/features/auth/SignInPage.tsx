import { useState, type FormEvent } from "react";
import { useLocation, useNavigate } from "react-router";
import { ApiError } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { Button, Field } from "@/shared/ui/ui";
import "./sign-in.css";

export function SignInPage() {
  const { signIn } = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const [credential, setCredential] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!credential.trim()) {
      setError("Paste an API key or access token to continue.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await signIn(credential);
      const from = (location.state as { from?: string } | null)?.from;
      navigate(from && from !== "/sign-in" ? from : "/", { replace: true });
    } catch (caught) {
      setError(
        caught instanceof ApiError && caught.status === 401
          ? "That credential was not accepted. It may be revoked, expired, or for a different Nimbus."
          : caught instanceof ApiError
            ? caught.explanation
            : "Sign-in failed unexpectedly.",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="sign-in">
      <div className="sign-in__card">
        <div className="sign-in__flow" aria-hidden="true">
          <span>Queue</span>
          <span>Lambda</span>
          <span>DLQ</span>
        </div>
        <h1>Sign in to Nimbus</h1>
        <p className="sign-in__lede">Watch your async pipelines, respond to incidents, and approve protected automations.</p>
        <form onSubmit={submit} noValidate>
          <Field label="API key or access token" hint="Your credential stays in this browser tab and is cleared when you close it." error={error ?? undefined}>
            <input
              type="password"
              name="credential"
              autoComplete="off"
              spellCheck={false}
              value={credential}
              onChange={(event) => setCredential(event.target.value)}
              autoFocus
            />
          </Field>
          <Button type="submit" variant="primary" busy={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>
      </div>
    </main>
  );
}
