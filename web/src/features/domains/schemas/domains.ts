import { z } from "zod";

/**
 * Domain form schemas (V9 sweep). The sweep found NO hand-written
 * client-side field validation in these forms: the certificate, provider
 * and redirect editors submit whatever the widgets produce and the server
 * validates (redirect domains: validateRedirectDomains in
 * internal/proxy/redirects.go; errors surface via describeProxyError into
 * the *Error refs). These schemas therefore model each draft shape with
 * exactly the current acceptance — every draft the UI can produce passes —
 * and stay the single source if a future change adds client gating.
 * The hand-written interfaces in api/proxy.ts are kept: they describe the
 * wire contract (optional vs required halves differ per endpoint), not just
 * the form.
 */
export const certificateDraftSchema = z.object({
  application_id: z.string(),
  challenge: z.union([z.literal("http-01"), z.literal("dns-01")]),
  dns_provider_id: z.string(),
  wildcard: z.boolean(),
  enabled: z.boolean(),
});

/** CertificateDraftInput is the certificate editor shape. */
export type CertificateDraftInput = z.infer<typeof certificateDraftSchema>;

/** isCertificateDraftValid accepts every UI-producible draft. */
export function isCertificateDraftValid(value: unknown): boolean {
  return certificateDraftSchema.safeParse(value).success;
}

export const providerFormSchema = z.object({
  provider: z.union([z.literal("cloudflare"), z.literal("digitalocean")]),
  name: z.string(),
  zones: z.array(z.string()),
  credential: z.string(),
  enabled: z.boolean(),
});

/** ProviderFormInput is the provider editor shape. */
export type ProviderFormInput = z.infer<typeof providerFormSchema>;

/** isProviderFormValid accepts every UI-producible draft. */
export function isProviderFormValid(value: unknown): boolean {
  return providerFormSchema.safeParse(value).success;
}

export const redirectFormSchema = z.object({
  application_id: z.string(),
  source_domain: z.string(),
  target_domain: z.string(),
  code: z.union([z.literal(301), z.literal(302)]),
  preserve_path: z.boolean(),
  enabled: z.boolean(),
});

/** RedirectFormInput is the redirect editor shape. */
export type RedirectFormInput = z.infer<typeof redirectFormSchema>;

/** isRedirectFormValid accepts every UI-producible draft. */
export function isRedirectFormValid(value: unknown): boolean {
  return redirectFormSchema.safeParse(value).success;
}
