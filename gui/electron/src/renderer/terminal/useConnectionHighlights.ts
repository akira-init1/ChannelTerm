import { useEffect, useState } from "react";
import {
  connectionHistoryKey,
  connectionHighlightsChanged,
  restoreConnections,
} from "../../shared/connection-history";
import type { HighlightRule } from "../../shared/highlight-rules";

export const emptyHighlightRules: readonly HighlightRule[] = [];

function readProfiles() {
  try {
    return {
      entries: restoreConnections(localStorage.getItem(connectionHistoryKey)),
      failed: false,
    };
  } catch {
    return { entries: [], failed: true };
  }
}

/** Local presentation follows explicit profile saves, including another window's storage event. */
export function useConnectionHighlights() {
  const [profiles, setProfiles] = useState(readProfiles);
  useEffect(() => {
    const changed = () => setProfiles(readProfiles());
    const storage = (event: StorageEvent) => {
      if (event.key === connectionHistoryKey || event.key === null) changed();
    };
    window.addEventListener(connectionHighlightsChanged, changed);
    window.addEventListener("storage", storage);
    return () => {
      window.removeEventListener(connectionHighlightsChanged, changed);
      window.removeEventListener("storage", storage);
    };
  }, []);
  return profiles;
}
