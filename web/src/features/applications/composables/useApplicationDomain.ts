import { hostDomainSchema } from "@/features/applications/schemas/applications";

/**
 * isValidDomain accepts an empty value (clears) or a plain hostname.
 * Schema-backed: see `schemas/applications.ts hostDomainSchema`.
 */
export function isValidDomain(value: string): boolean {
  return hostDomainSchema.safeParse(value).success;
}
