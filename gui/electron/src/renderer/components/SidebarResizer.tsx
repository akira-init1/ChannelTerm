import { useRef, useState } from "react";

/** Local layout only: resizing never changes attachments or remote terminal size. */
export function SidebarResizer({
  width,
  maximum,
  defaultWidth,
  label,
  hint,
  onResize,
}: {
  width: number;
  maximum: number;
  defaultWidth: number;
  label: string;
  hint: string;
  onResize: (width: number) => void;
}) {
  const drag = useRef<{ id: number; x: number; width: number } | undefined>(
    undefined,
  );
  const [dragging, setDragging] = useState(false);
  const resize = (next: number) =>
    onResize(Math.max(180, Math.min(maximum, next)));
  const finish = () => {
    drag.current = undefined;
    setDragging(false);
  };
  return (
    <div
      className={`sidebar-resizer ${dragging ? "dragging" : ""}`}
      role="separator"
      aria-label={label}
      aria-orientation="vertical"
      aria-controls="session-sidebar"
      aria-valuemin={180}
      aria-valuemax={maximum}
      aria-valuenow={Math.round(width)}
      title={hint}
      tabIndex={0}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.currentTarget.focus();
        event.currentTarget.setPointerCapture(event.pointerId);
        drag.current = { id: event.pointerId, x: event.clientX, width };
        setDragging(true);
      }}
      onPointerMove={(event) => {
        if (drag.current?.id === event.pointerId)
          resize(drag.current.width + event.clientX - drag.current.x);
      }}
      onPointerUp={(event) => {
        if (drag.current?.id !== event.pointerId) return;
        finish();
        event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onPointerCancel={finish}
      onLostPointerCapture={finish}
      onDoubleClick={() => resize(defaultWidth)}
      onKeyDown={(event) => {
        if (
          event.nativeEvent.isComposing ||
          event.keyCode === 229 ||
          event.altKey ||
          event.ctrlKey ||
          event.metaKey
        )
          return;
        const next =
          event.key === "ArrowLeft"
            ? width - 10
            : event.key === "ArrowRight"
              ? width + 10
              : event.key === "Home"
                ? 180
                : event.key === "End"
                  ? maximum
                  : undefined;
        if (next === undefined) return;
        event.preventDefault();
        event.stopPropagation();
        resize(next);
      }}
    />
  );
}
