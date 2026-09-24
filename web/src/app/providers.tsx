import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { ApiError } from "@/shared/api/client";
import { SessionProvider } from "@/shared/auth/session";

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        // Client errors will not fix themselves; only retry transient failures.
        retry: (count, error) => count < 2 && (!(error instanceof ApiError) || error.status === 0 || error.status >= 500),
        refetchOnWindowFocus: true,
      },
    },
  });
}

export function Providers({ children, client }: { children: ReactNode; client?: QueryClient }) {
  const [queryClient] = useState(() => client ?? createQueryClient());
  return (
    <QueryClientProvider client={queryClient}>
      <SessionProvider>{children}</SessionProvider>
    </QueryClientProvider>
  );
}
