import { createRoot } from "react-dom/client";
import { StrictMode } from "react";
import { App } from "./components/App";
import windowLogo from "../../../../assets/brand/generated/channelterm-window.png?inline";
import "@xterm/xterm/css/xterm.css";
import "./styles/app.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/^Win/i.test(navigator.platform) && (
      <div className="window-titlebar">
        <img src={windowLogo} alt="" draggable={false} />
        <span>{document.title}</span>
      </div>
    )}
    <App />
  </StrictMode>,
);
