import type { SessionSummary } from "../../shared/api";
import { useState, type MouseEvent } from "react";
import { Icon } from "../components/Icon";

/**
 * SessionList presents Host snapshots and reports selection to the shell.
 * Opaque IDs identify rows; short references and labels are display metadata.
 * Disabled rows are a usability guard, not authority over Session lifecycle.
 */
export function SessionList({
  sessions,
  selected,
  disabled,
  emptyMessage,
  onSelect,
  onContextMenu,
}: {
  sessions: SessionSummary[];
  selected?: string;
  disabled: boolean;
  emptyMessage: string;
  onSelect: (session: SessionSummary) => void;
  onContextMenu: (
    event: MouseEvent<HTMLElement>,
    session: SessionSummary,
  ) => void;
}) {
  // Filtering only changes visible rows; it never detaches a hidden selection.
  const [query, setQuery] = useState("");
  const search = query.trim().toLowerCase();
  const groups = new Map<string, SessionSummary[]>();
  for (const session of sessions) {
    if (
      ![
        session.session_ref,
        session.label,
        session.endpoint,
        session.transport,
      ].some((value) => value.toLowerCase().includes(search))
    )
      continue;
    const group =
      session.transport === "serial"
        ? "Serial"
        : session.transport === "ssh"
          ? "SSH"
          : "Other";
    const entries = groups.get(group) ?? [];
    entries.push(session);
    groups.set(group, entries);
  }
  return (
    <div className="session-browser">
      <label className="session-search">
        <Icon name="search" />
        <input
          type="search"
          aria-label="Search Sessions"
          placeholder="Search Sessions…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </label>
      <nav aria-label="Sessions" className="session-list">
        {sessions.length === 0 && <p className="empty-list">{emptyMessage}</p>}
        {sessions.length > 0 && groups.size === 0 && (
          <p className="empty-list" role="status">
            No matching Sessions.
          </p>
        )}
        {["Serial", "SSH", "Other"].map((group) => {
          const entries = groups.get(group);
          if (!entries) return null;
          return (
            <details className="session-group" key={group} open>
              <summary>
                <Icon name="chevron" />
                {group} <span>({entries.length})</span>
              </summary>
              {entries.map((session) => (
                <div
                  key={session.session_id}
                  onContextMenu={(event) => onContextMenu(event, session)}
                >
                  <button
                    key={session.session_id}
                    className={`session ${selected === session.session_id ? "selected" : ""}`}
                    aria-pressed={selected === session.session_id}
                    title={`${session.session_ref} · ${session.label || session.endpoint} · ${session.state}`}
                    disabled={disabled || session.state !== "open"}
                    onClick={() => onSelect(session)}
                  >
                    <span className={`session-symbol ${session.transport}`}>
                      <Icon
                        name={
                          session.transport === "serial" ? "serial" : "monitor"
                        }
                      />
                    </span>
                    <span className="session-content">
                      <span className="session-title">
                        <strong>
                          {session.label || session.session_ref || "Terminal"}
                        </strong>
                        <span
                          className={`dot ${session.state}`}
                          aria-hidden="true"
                        />
                      </span>
                      <span className="session-label">
                        {session.endpoint || session.session_ref}
                      </span>
                      <span className="session-meta">
                        {session.session_ref} · {session.state}
                        {session.lease ? ` · busy: ${session.lease.type}` : ""}
                      </span>
                    </span>
                  </button>
                </div>
              ))}
            </details>
          );
        })}
      </nav>
    </div>
  );
}
