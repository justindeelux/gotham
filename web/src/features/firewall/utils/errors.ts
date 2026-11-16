import { errorCode, errorParams, errorText } from "@/shared/api/errors";

import type { FirewallStatus } from "../api/firewall";
import en from "../locales/en";

// Every code the API returns maps to `firewall.errors.<code>`; both catalogs
// carry the same keys.
const knownCodes: Record<string, string> = en.errors;
const accessLabels: Record<string, string> = en.access;

export type Translate = (key: string, params?: Record<string, string>) => string;

function localizedParams(
  params: Record<string, string>,
  t: Translate,
): Record<string, string> {
  const access = params.access;
  if (access === undefined || !(access in accessLabels)) {
    return params;
  }
  return { ...params, access: t(`firewall.access.${access}`) };
}

// firewallErrorText translates a known code and keeps the server message for
// an unknown one, so an older agent never shows a raw key or a blank alert.
export function firewallErrorText(
  error: unknown,
  t: Translate,
  fallback: string,
): string {
  const code = errorCode(error);
  if (code !== "" && code in knownCodes) {
    return t(`firewall.errors.${code}`, localizedParams(errorParams(error), t));
  }
  return errorText(error) || fallback;
}

// firewallDetailText translates why a node's firewall cannot be managed.
export function firewallDetailText(
  status: FirewallStatus,
  t: Translate,
): string {
  const code = status.detail_code ?? "";
  if (code !== "" && code in knownCodes) {
    return t(`firewall.errors.${code}`, localizedParams(status.detail_params ?? {}, t));
  }
  return status.detail || t("firewall.status.unsupportedBody");
}
