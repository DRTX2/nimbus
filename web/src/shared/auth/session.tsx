import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, use, useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { api, configureClient, unwrap } from "@/shared/api/client";

const STORAGE_KEY = "nimbus.credential";

export type Principal = {
  id: string;
  name: string;
  role: string;
  tenantId: string;
  permissions: string[];
};

type SessionValue = {
  principal: Principal | null;
  status: "anonymous" | "loading" | "authenticated";
  signIn: (credential: string) => Promise<Principal>;
  signOut: () => void;
  can: (permission: string) => boolean;
};

const SessionContext = createContext<SessionValue | null>(null);

// Credentials live in sessionStorage: they survive reloads but not the tab,
// and are never written to localStorage or cookies readable elsewhere.
function readCredential(): string | null {
  try {
    return sessionStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeCredential(value: string | null): void {
  try {
    if (value) sessionStorage.setItem(STORAGE_KEY, value);
    else sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage can be unavailable (private mode); the session then lasts until reload.
  }
}

let memoryCredential: string | null = null;

async function fetchPrincipal(): Promise<Principal> {
  const me = await unwrap(api.GET("/v1/me"));
  return { id: me.id, name: me.name, role: me.role, tenantId: me.tenantId, permissions: me.permissions ?? [] };
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [credential, setCredential] = useState<string | null>(() => {
    memoryCredential = readCredential();
    return memoryCredential;
  });

  const signOut = useCallback(() => {
    memoryCredential = null;
    writeCredential(null);
    setCredential(null);
    queryClient.clear();
  }, [queryClient]);

  useEffect(() => {
    configureClient(() => memoryCredential, signOut);
  }, [signOut]);
  configureClient(() => memoryCredential, signOut);

  const me = useQuery({ queryKey: ["me", credential], queryFn: fetchPrincipal, enabled: credential !== null, retry: false, staleTime: 60_000 });

  const signIn = useCallback(
    async (value: string) => {
      memoryCredential = value.trim();
      try {
        const principal = await fetchPrincipal();
        writeCredential(memoryCredential);
        setCredential(memoryCredential);
        queryClient.setQueryData(["me", memoryCredential], principal);
        return principal;
      } catch (error) {
        memoryCredential = null;
        throw error;
      }
    },
    [queryClient],
  );

  const value = useMemo<SessionValue>(() => {
    const principal = credential ? (me.data ?? null) : null;
    const status = !credential ? "anonymous" : principal ? "authenticated" : me.isError ? "anonymous" : "loading";
    return {
      principal,
      status,
      signIn,
      signOut,
      can: (permission) => principal?.permissions.includes(permission) ?? false,
    };
  }, [credential, me.data, me.isError, signIn, signOut]);

  return <SessionContext value={value}>{children}</SessionContext>;
}

export function useSession(): SessionValue {
  const value = use(SessionContext);
  if (!value) throw new Error("useSession must be used inside SessionProvider");
  return value;
}
