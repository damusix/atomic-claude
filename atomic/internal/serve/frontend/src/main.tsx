import { createRoot } from "react-dom/client";
import { App } from "./App";

const rootEl = document.getElementById("root");

if (rootEl) {
  createRoot(rootEl).render(<App />);
}
