import type { ClientError } from "./api";

/** Translate structured local diagnostics; never classify an error by its text. */
export function hostRecoveryMessage(
  error: ClientError,
  t: (id: string) => string,
): string {
  switch (error.code) {
    case "host_legacy_credentials":
    case "host_identity_missing":
    case "host_credential_missing":
    case "host_credential_invalid":
    case "host_credential_unreadable":
    case "host_companion_outdated":
      return t(`recovery.${error.code}`);
    default:
      return error.message;
  }
}
