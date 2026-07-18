import { createRoot } from "react-dom/client";
import { App } from "./App";
import { installTypeColorsGlobal } from "./utils/typeColors";

// The carried public/ scripts (graph-core.js, system-graph.js, code-graph.js)
// are plain, non-module <script>s that call atomicCyTypeColors()/
// atomicRampColors() unqualified — install the window globals before they
// can run.
installTypeColorsGlobal();

const rootEl = document.getElementById("root");

if (rootEl) {
  createRoot(rootEl).render(<App />);
}
