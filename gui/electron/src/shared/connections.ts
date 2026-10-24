/** Explicit connection intent only; Host owns transport setup and shared lifetime. */
export type ConnectionRequest =
  | {
      kind: "serial";
      port: string;
      baud: number;
      dataBits: number;
      parity: string;
      stopBits: string;
      flowControl: string;
      label: string;
    }
  | {
      kind: "ssh";
      host: string;
      user: string;
      port: number;
      identityFiles: string[];
      knownHostsFile: string;
      password?: string;
      keyPassphrase?: string;
      label: string;
    };

export interface OpenedConnection {
  sessionId: string;
  reused: boolean;
}

function bounded(value: unknown, max: number, empty = false): value is string {
  return (
    typeof value === "string" &&
    (empty || value.trim().length > 0) &&
    value.length <= max &&
    !/[\x00-\x1f\x7f]/.test(value)
  );
}
function numberIn(value: unknown, min: number, max: number): boolean {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= min &&
    value <= max
  );
}

/** Main repeats this shape check; disabled menus and form validation grant no authority. */
export function isConnectionRequest(
  value: unknown,
): value is ConnectionRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  if (!bounded(v.label, 128, true)) return false;
  const exact = (keys: string[]) =>
    Object.keys(v).every((key) => keys.includes(key));
  if (v.kind === "serial")
    return (
      exact([
        "kind",
        "port",
        "baud",
        "dataBits",
        "parity",
        "stopBits",
        "flowControl",
        "label",
      ]) &&
      bounded(v.port, 512) &&
      numberIn(v.baud, 1, 4000000) &&
      numberIn(v.dataBits, 5, 8) &&
      ["none", "odd", "even", "mark", "space"].some(
        (item) => item === v.parity,
      ) &&
      ["1", "1.5", "2"].some((item) => item === v.stopBits) &&
      ["none", "software", "hardware"].some((item) => item === v.flowControl)
    );
  if (v.kind !== "ssh") return false;
  return (
    exact([
      "kind",
      "host",
      "user",
      "port",
      "identityFiles",
      "knownHostsFile",
      "password",
      "keyPassphrase",
      "label",
    ]) &&
    bounded(v.host, 253) &&
    !/[@/\[\]\s]/.test(v.host) &&
    bounded(v.user, 128) &&
    !/[@\s]/.test(v.user) &&
    numberIn(v.port, 1, 65535) &&
    bounded(v.knownHostsFile, 4096, true) &&
    Array.isArray(v.identityFiles) &&
    v.identityFiles.length <= 8 &&
    v.identityFiles.every((path) => bounded(path, 4096)) &&
    [v.password, v.keyPassphrase].every(
      (secret) =>
        secret === undefined ||
        (typeof secret === "string" && secret.length <= 4096),
    )
  );
}
