import { useEffect, useRef, useState } from "react";
import { clientErrorMessageId } from "../../shared/host-recovery";
import type { SerialPort } from "../../shared/devices";
import { host, unwrap } from "../host";

/** Editable port name with suggestions from the Host's read-only enumeration. */
export function SerialPortInput({
  defaultValue,
  scannedPorts,
  onEdit,
  t,
}: {
  defaultValue?: string;
  scannedPorts?: SerialPort[];
  onEdit: () => void;
  t: (id: string) => string;
}) {
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const [open, setOpen] = useState(false);
  const [ports, setPorts] = useState<SerialPort[]>();
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [active, setActive] = useState(-1);
  useEffect(() => {
    if (!open || scannedPorts) return;
    let current = true;
    setError("");
    setPorts(undefined);
    void unwrap(host.serialPorts()).then(
      (result) => {
        if (current) setPorts(result);
      },
      (reason) => {
        if (current) setError(clientErrorMessageId(reason));
      },
    );
    return () => {
      current = false;
    };
  }, [open, scannedPorts]);

  const available = scannedPorts ?? ports;
  const query = filter.toLowerCase();
  const options =
    available?.filter(
      ({ name, product }) =>
        name.toLowerCase().includes(query) ||
        product.toLowerCase().includes(query),
    ) ?? [];
  const expanded = open && (!available?.length || options.length > 0);
  function show(value: string) {
    setFilter(value);
    setActive(-1);
    setOpen(true);
  }
  function choose(port: string) {
    input.current!.value = port;
    onEdit();
    setOpen(false);
  }
  return (
    <div className="serial-port-picker">
      <input
        ref={input}
        id="serial-port"
        name="port"
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={expanded}
        aria-controls={expanded ? "serial-ports" : undefined}
        aria-activedescendant={
          expanded && options[active] ? `serial-port-${active}` : undefined
        }
        defaultValue={defaultValue}
        required
        maxLength={512}
        onClick={() => show("")}
        onBlur={() => setOpen(false)}
        onChange={(event) => {
          onEdit();
          show(event.currentTarget.value);
        }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            if (!expanded) {
              show("");
              return;
            }
            if (!options.length) return;
            const next =
              event.key === "ArrowDown"
                ? Math.min(active + 1, options.length - 1)
                : active <= 0
                  ? options.length - 1
                  : active - 1;
            setActive(next);
            list.current?.children[next]?.scrollIntoView({ block: "nearest" });
          } else if (open && event.key === "Enter" && options[active]) {
            event.preventDefault();
            choose(options[active].name);
          } else if (open && event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            setOpen(false);
          }
        }}
      />
      {expanded && (
        <ul
          ref={list}
          id="serial-ports"
          role="listbox"
          aria-label={t("connection.port")}
        >
          {options.map(({ name, product }, index) => (
            <li
              key={name}
              id={`serial-port-${index}`}
              role="option"
              aria-selected={index === active}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose(name)}
            >
              {product && product !== name ? `${name} — ${product}` : name}
            </li>
          ))}
          {!options.length && (
            <li role="presentation">
              {t(
                error || (available ? "devices.empty" : "connection.scanning"),
              )}
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
