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

/** sourceTypeImplemented gates the wizard Source step on scope: public
 * git, the connected-provider flows and pasted Dockerfiles (GS-7).
 * git_private waits for GS-4 and the remaining container sources for
 * GS-8..GS-9. */
export function sourceTypeImplemented(value: string): boolean {
  return (
    value === "git_public" ||
    value === "github_app" ||
    value === "gitlab_app" ||
    value === "dockerfile"
  );
}

/**
 * maxDockerfileBytes caps pasted Dockerfile text at 64 KiB, matching
 * MaxDockerfileBytes in internal/deploy/dockerfile.go.
 */
export const maxDockerfileBytes = 64 * 1024;

/**
 * dockerfileContentSchema replaces the GS-7 creation gate on the trimmed
 * value: non-empty, within the size limit, containing a FROM instruction
 * (comments and blank lines ignored, case-insensitive first word).
 * Gate-only: consumers read `.success`, deeper validation happens on the
 * node at build time.
 */
export const dockerfileContentSchema = z
  .string()
  .trim()
  .min(1)
  .max(maxDockerfileBytes)
  .refine((value) =>
    value.split("\n").some((line) => {
      const trimmed = line.trim();
      if (trimmed === "" || trimmed.startsWith("#")) {
        return false;
      }
      const [first] = trimmed.split(/\s+/);
      return first?.toUpperCase() === "FROM";
    }),
  );

/** buildArgKeySchema replaces the key check for one --build-arg pair: the
 * same container-variable shape the API enforces (no spaces, '=' or NUL,
 * 128 chars max). Gate-only. */
export const buildArgKeySchema = z
  .string()
  .trim()
  .min(1)
  .max(128)
  .refine((value) => !/[= \t\r\n\0]/.test(value));

/** buildArgValueSchema bounds one --build-arg value at 4 KiB. Gate-only. */
export const buildArgValueSchema = z.string().max(4 * 1024);

/** cloneUrlSchema replaces cloneUrl.trim() !== "". Gate-only. */
export const cloneUrlSchema = z.string().trim().min(1);

/**
 * isPublicGitUrl mirrors ValidatePublicGitURL's production allow-list
 * (internal/deploy/gitpublic.go): a keyless public source takes http, https
 * or git with a host and no embedded credentials. SSH transports need a
 * deploy key, so the gate refuses them here instead of letting the clone
 * spend the ambient SSH identity.
 */
export function isPublicGitUrl(value: string): boolean {
  const trimmed = value.trim();
  // Whitespace and control characters can smuggle a second argv token or a
  // newline into logs; the loop form keeps the control range out of a regex
  // literal (no-control-regex).
  if (trimmed === "" || /\s/.test(trimmed)) {
    return false;
  }
  for (const ch of trimmed) {
    const code = ch.codePointAt(0) ?? 0;
    if (code <= 0x1f || code === 0x7f) {
      return false;
    }
  }
  // The authority follows "://" directly: a leading slash means there is no
  // host (this also keeps WHATWG parsing aligned with Go's net/url, which
  // reads "https:///o/r.git" as hostless).
  const sep = trimmed.indexOf("://");
  const afterScheme = sep < 0 ? "" : trimmed.slice(sep + 3);
  if (afterScheme === "" || afterScheme.startsWith("/")) {
    return false;
  }
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return false;
  }
  const scheme = parsed.protocol.toLowerCase();
  if (scheme !== "http:" && scheme !== "https:" && scheme !== "git:") {
    return false;
  }
  if (parsed.hostname === "") {
    return false;
  }
  return parsed.username === "" && parsed.password === "";
}

/** publicCloneUrlSchema gates the git_public URL field. Gate-only. */
export const publicCloneUrlSchema = z
  .string()
  .trim()
  .min(1)
  .refine(isPublicGitUrl);

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
