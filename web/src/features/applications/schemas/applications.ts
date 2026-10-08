import { z } from "zod";

/**
 * Application form schemas (V9 sweep). Every predicate below replaces one
 * hand-written check in `useCreateAppWizard` or `useApplicationDomain` with
 * the identical acceptance; the composables derive their gates from these
 * schemas so there is a single source of truth.
 *
 * Regex exceptions (deliberate, historic patterns kept verbatim):
 * NAME_PATTERN, WIZARD_DOMAIN_PATTERN and HOST_PATTERN stay inside plain
 * `.regex`/`.refine` rules because they ARE the exact historic expressions —
 * re-spelling them in another form would risk acceptance drift. Only
 * hostDomainSchema carries a user-visible message; every other schema below
 * is gate-only (consumers read `.success`), so they use zod's default
 * messages and have no catalog entries.
 */

/**
 * Message catalog: only strings the UI shows. Gate-only schemas use zod
 * defaults (never surfaced). The domain message is a namespaced catalog key
 * resolved at invocation time, so visible feedback follows a language switch.
 */
export const applicationMessages = {
  domain: "applications.validation.domain",
} as const;

const NAME_PATTERN = /^[a-z][a-z0-9-]{2,30}$/;
const WIZARD_DOMAIN_PATTERN = /^[a-z0-9.-]+\.[a-z]{2,}$/;
/** HOST_PATTERN mirrors the generator's ValidateDomain boundary. */
const HOST_PATTERN = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;

/** appNameSchema replaces NAME_PATTERN.test(name.trim()) in sourceValid. */
export const appNameSchema = z.string().trim().regex(NAME_PATTERN);

/** branchSchema replaces branch.trim() !== "". Gate-only. */
export const branchSchema = z.string().trim().min(1);

/** providerSchema replaces providerId === "" (a select output, never padded). */
export const providerSchema = z.string().min(1);

/**
 * sourceTypeSchema is the GS-2 application source model: which fetcher the
 * deploy orchestrator uses. Only git-backed types are implemented; the
 * wizard gates continuation on sourceTypeImplemented below.
 */
export const sourceTypeSchema = z.enum([
  "git_public",
  "git_private",
  "github_app",
  "gitlab_app",
  "dockerfile",
  "compose",
  "image",
]);

export type SourceType = z.infer<typeof sourceTypeSchema>;

/** sourceTypeImplemented gates the wizard Source step on GS-2 scope: public
 * git and the connected-provider flows. git_private waits for GS-4 and the
 * container sources for GS-7..GS-9. */
export function sourceTypeImplemented(value: string): boolean {
  return (
    value === "git_public" ||
    value === "github_app" ||
    value === "gitlab_app"
  );
}

/** cloneUrlSchema replaces cloneUrl.trim() !== "". Gate-only. */
export const cloneUrlSchema = z.string().trim().min(1);

/** repoSchema replaces repoFullName === "" (a select output, never padded). */
export const repoSchema = z.string().min(1);

/** serverSchema replaces serverId === "" (a select output, never padded). */
export const serverSchema = z.string().min(1);

/**
 * hostPortSchema replaces the host-port check: null means auto-assigned and
 * passes, otherwise an integer 0-65535 is required. Gate-only. The container
 * port reuses the shared portSchema from primitives (same 1-65535 shape).
 */
export const hostPortSchema = z.number().int().min(0).max(65535).nullable();

/**
 * wizardDomainSchema replaces `domain === "" || DOMAIN_PATTERN.test(domain)`
 * on the trimmed value: empty clears the domain, otherwise the pattern holds.
 * Gate-only.
 */
export const wizardDomainSchema = z
  .string()
  .trim()
  .refine((value) => value === "" || WIZARD_DOMAIN_PATTERN.test(value));

/**
 * hostDomainSchema mirrors isValidDomain exactly: the caller lowercases and
 * trims first, so the schema takes the value as-is — empty clears, otherwise
 * 253 chars max and the hostname shape.
 */
export const hostDomainSchema = z.string().refine(
  (value) => value === "" || (value.length <= 253 && HOST_PATTERN.test(value)),
  { message: applicationMessages.domain },
);
