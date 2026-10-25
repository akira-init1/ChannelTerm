/** Display identity only. The Host challenge capability never crosses IPC. */
export interface SSHTrustPrompt {
  id: string;
  address: string;
  algorithm: string;
  fingerprint: string;
}

export function isSSHTrustPrompt(value: unknown): value is SSHTrustPrompt {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    Object.keys(v).length === 4 &&
    typeof v.id === "string" &&
    /^[a-zA-Z0-9-]{1,64}$/.test(v.id) &&
    typeof v.address === "string" &&
    v.address.length > 0 &&
    v.address.length <= 512 &&
    !/[\x00-\x20\x7f]/.test(v.address) &&
    typeof v.algorithm === "string" &&
    /^[a-zA-Z0-9@._+-]{1,128}$/.test(v.algorithm) &&
    typeof v.fingerprint === "string" &&
    /^SHA256:[A-Za-z0-9+/]{43}$/.test(v.fingerprint)
  );
}
