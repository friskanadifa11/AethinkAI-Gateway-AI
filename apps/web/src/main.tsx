import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import "./index.css";

const container = document.getElementById("root");
if (!container) {
  // A missing mount point is a build defect, not a runtime condition, so say so
  // loudly rather than failing silently with a blank page.
  throw new Error("Root element #root is missing from index.html");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
