import {
  Bug,
  ChevronDown,
  FolderLock,
  Network,
  LockKeyhole,
  LockKeyholeOpen,
  PanelBottom,
  PanelLeft,
  PanelRight,
  Plug,
  RefreshCw,
  Search,
  SquareTerminal,
  Terminal,
  X,
} from "lucide-react";

const glyphs = {
  debug: Bug,
  terminal: SquareTerminal,
  search: Search,
  refresh: RefreshCw,
  close: X,
  chevron: ChevronDown,
  plug: Plug,
  "layout-left": PanelLeft,
  "layout-right": PanelRight,
  "layout-bottom": PanelBottom,
  shell: Terminal,
  lan: Network,
  locked: LockKeyhole,
  unlocked: LockKeyholeOpen,
  tcp: Network,
  sftp: FolderLock,
  telnet: SquareTerminal,
};

// The Serial connector and SSH monitor are the existing Session glyphs.
const sessionPaths = {
  serial:
    "M5 5h14l2 8-4 4H7l-4-4z M8 9h.01 M12 9h.01 M16 9h.01 M10 13h.01 M14 13h.01 M12 17v4 M8 21h8",
  monitor: "M3 4h18v13H3z M12 17v4 M7 21h10",
};

export type IconName = keyof typeof glyphs | keyof typeof sessionPaths | "wsl";

/** Icon supplies decorative workbench glyphs; interactive parents own labels. */
export function Icon({ name }: { name: IconName }) {
  if (name === "wsl")
    return <span className="icon icon-windows" aria-hidden="true" />;
  if (name === "serial" || name === "monitor")
    return (
      <svg
        className="icon"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d={sessionPaths[name]} />
      </svg>
    );
  const Glyph = glyphs[name];
  return <Glyph className="icon" strokeWidth={1.6} aria-hidden="true" />;
}

export function TransportIcon({
  transport,
  t,
}: {
  transport: string;
  t: (id: string) => string;
}) {
  const label =
    transport === "serial"
      ? t("debug.serial")
      : transport === "ssh"
        ? "SSH"
        : transport;
  return (
    <span
      className="directory-transport"
      role="img"
      aria-label={label}
      title={label}
    >
      <Icon
        name={
          transport === "serial"
            ? "serial"
            : transport === "ssh"
              ? "monitor"
              : "shell"
        }
      />
    </span>
  );
}
