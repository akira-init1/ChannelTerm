import { createRoot } from "react-dom/client";
import { App } from "./components/App";
import "@xterm/xterm/css/xterm.css";
import "./styles/app.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
import { StrictMode } from "react";
