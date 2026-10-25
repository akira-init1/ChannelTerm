/** Optional initial SSH transport settings, never shared resize ownership. */
export interface SSHAdvanced {
  timeoutSeconds: number;
  keepAliveSeconds: number;
  keepAliveCount: number;
  writeTimeoutSeconds: number;
  term: string;
  columns: number;
  rows: number;
}
export const defaultSSHAdvanced: Readonly<SSHAdvanced> = {
  timeoutSeconds: 10,
  keepAliveSeconds: 0,
  keepAliveCount: 3,
  writeTimeoutSeconds: 0,
  term: "xterm-256color",
  columns: 80,
  rows: 24,
};

/** Explicit connection intent only; Host owns transport setup and shared lifetime. */
export type ConnectionRequest =
  | {
      kind: "serial";
      wake?: boolean;
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
      advanced?: SSHAdvanced;
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

export function isSSHAdvanced(value: unknown): value is SSHAdvanced {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    Object.keys(v).length === 7 &&
    numberIn(v.timeoutSeconds, 1, 120) &&
    numberIn(v.keepAliveSeconds, 0, 3600) &&
    numberIn(v.keepAliveCount, 1, 10) &&
    numberIn(v.writeTimeoutSeconds, 0, 3600) &&
    typeof v.term === "string" &&
    /^[a-zA-Z0-9_.+-]{1,64}$/.test(v.term) &&
    numberIn(v.columns, 1, 1000) &&
    numberIn(v.rows, 1, 1000)
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
        "wake",
        "port",
        "baud",
        "dataBits",
        "parity",
        "stopBits",
        "flowControl",
        "label",
      ]) &&
      (v.wake === undefined || typeof v.wake === "boolean") &&
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
      "advanced",
      "label",
    ]) &&
    bounded(v.host, 253) &&
    !/[@/\[\]\s]/.test(v.host) &&
    bounded(v.user, 128) &&
    !/[@\s]/.test(v.user) &&
    numberIn(v.port, 1, 65535) &&
    (v.advanced === undefined || isSSHAdvanced(v.advanced)) &&
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
