// Route tree — shared between the production browser router (App.tsx) and
// tests (createMemoryRouter over the same tree) so route wiring is exercised
// exactly as it ships. Preserves today's URL scheme: /page/<relpath>, /graph,
// /search, /status, /external, /code/schema, "/" landing.
import type { RouteObject } from "react-router";
import { Shell } from "./layouts/Shell/Shell";
import { External } from "./pages/External";
import { Graph } from "./pages/Graph/Graph";
import { Page } from "./pages/Page/Page";
import { Schema } from "./pages/Schema";
import { Search } from "./pages/Search";
import { Status } from "./pages/Status";

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <Shell />,
    children: [
      { index: true, element: <Page /> },
      { path: "page/*", element: <Page /> },
      { path: "graph", element: <Graph /> },
      { path: "search", element: <Search /> },
      { path: "status", element: <Status /> },
      { path: "external", element: <External /> },
      { path: "code/schema", element: <Schema /> },
    ],
  },
];
