// Route tree — shared between the production browser router (App.tsx) and
// tests (createMemoryRouter over the same tree) so route wiring is exercised
// exactly as it ships. Preserves today's URL scheme: /page/<relpath>, /graph,
// /search, /status, /external, /code/schema, "/" landing.
import type { RouteObject } from "react-router";
import { Shell } from "./layouts/Shell/Shell";
import { Bus } from "./pages/Bus/Bus";
import { External } from "./pages/External";
import { Graph } from "./pages/Graph/Graph";
import { Page } from "./pages/Page/Page";
import { NotFoundRoute, RouteErrorBoundary } from "./pages/RouteError";
import { Schema } from "./pages/Schema";
import { Search } from "./pages/Search";
import { Status } from "./pages/Status";

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <Shell />,
    // Both are on the shell route so a failure renders inside the app rather
    // than replacing the document with the router's developer screen — an
    // unmatched path leaves the nav, search and mode rail intact.
    errorElement: <RouteErrorBoundary />,
    children: [
      { index: true, element: <Page /> },
      { path: "page/*", element: <Page /> },
      { path: "graph", element: <Graph /> },
      { path: "bus", element: <Bus /> },
      { path: "search", element: <Search /> },
      { path: "status", element: <Status /> },
      { path: "external", element: <External /> },
      { path: "code/schema", element: <Schema /> },
      { path: "*", element: <NotFoundRoute /> },
    ],
  },
];
