import { fileURLToPath, URL } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The dashboard is served by the Nimbus API at /app/ in every environment, so
// requests stay same-origin and the strict CSP needs no exceptions.
export default defineConfig({
  base: "/app/",
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: 5173,
    proxy: { "/v1": process.env.NIMBUS_API_URL ?? "http://localhost:8080" },
  },
  build: { outDir: "dist", sourcemap: true, assetsInlineLimit: 0 },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
  },
});
