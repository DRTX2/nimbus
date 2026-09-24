import "@fontsource-variable/instrument-sans";
import "@fontsource/jetbrains-mono/400.css";
import "./styles/tokens.css";
import "./styles/base.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router";
import { Providers } from "./app/providers";
import { routes } from "./app/router";

const router = createBrowserRouter(routes, { basename: "/app" });

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

createRoot(root).render(
  <StrictMode>
    <Providers>
      <RouterProvider router={router} />
    </Providers>
  </StrictMode>,
);
