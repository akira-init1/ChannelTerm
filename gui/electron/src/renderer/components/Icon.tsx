/** Icon supplies decorative workbench glyphs; interactive parents own labels. */
export function Icon({
  name,
}: {
  name:
    | "terminal"
    | "serial"
    | "monitor"
    | "search"
    | "refresh"
    | "close"
    | "chevron"
    | "plug";
}) {
  const paths = {
    terminal: "M4 4h16v16H4z M7 8l3 3-3 3 M13 15h4",
    serial:
      "M5 5h14l2 8-4 4H7l-4-4z M8 9h.01 M12 9h.01 M16 9h.01 M10 13h.01 M14 13h.01 M12 17v4 M8 21h8",
    monitor: "M3 4h18v13H3z M12 17v4 M7 21h10",
    search: "M16 16l5 5 M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
    refresh:
      "M20 7v5h-5 M4 17v-5h5 M5 8a8 8 0 0 1 14-2l1 2 M4 16l1 2a8 8 0 0 0 14-2",
    close: "M6 6l12 12 M18 6L6 18",
    chevron: "M7 9l5 5 5-5",
    plug: "M9 3v5 M15 3v5 M7 8h10v4a5 5 0 0 1-10 0z M12 17v4",
  };
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
      <path d={paths[name]} />
    </svg>
  );
}
