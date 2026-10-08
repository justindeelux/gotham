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
 * git, the connected-provider flows, pasted Dockerfiles (GS-7) and prebuilt
 * container images (GS-9). git_private waits for GS-4 and Compose for GS-8. */
export function sourceTypeImplemented(value: string): boolean {
  return (
    value === "git_public" ||
    value === "github_app" ||
    value === "gitlab_app" ||
    value === "dockerfile" ||
    value === "image"
  );
}

/**
 * maxDockerfileBytes caps pasted Dockerfile text at 64 KiB, matching
 * MaxDockerfileBytes in internal/deploy/dockerfile.go.
 */
export const maxDockerfileBytes = 64 * 1024;

/**
 * dockerfileContentSchema replaces the GS-7 creation gate: the trimmed value
 * must be non-empty with a FROM instruction (comments and blank lines
 * ignored, case-insensitive first word), while the size cap applies to the
 * raw bytes, exactly like the server's len() check — trailing whitespace
 * counts. Bytes are measured with TextEncoder so multi-byte text matches;
 * deeper validation happens on the node at build time.
 * Gate-only: consumers read `.success`.
 */
export const dockerfileContentSchema = z
  .string()
  .refine((value) => value.trim().length > 0)
  .refine((value) => new TextEncoder().encode(value).length <= maxDockerfileBytes)
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

/**
 * maxBuildArgValueBytes caps one --build-arg value at 4 KiB, matching
 * MaxBuildArgValueBytes in internal/deploy/dockerfile.go.
 */
export const maxBuildArgValueBytes = 4 * 1024;

/** buildArgValueSchema bounds one --build-arg value in bytes (TextEncoder,
 * like the server) and rejects NUL, which Postgres jsonb refuses. Gate-only. */
export const buildArgValueSchema = z
  .string()
  .refine((value) => new TextEncoder().encode(value).length <= maxBuildArgValueBytes)
  .refine((value) => !value.includes("\0"));

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

/**
 * isImageRef mirrors ValidateImageReference (internal/deploy/image.go):
 * [host[:port]/]path[:tag][@sha256:hex], no whitespace, lowercase path
 * components, a valid tag when present, and never a loopback registry host
 * (the node-local registry scope). A bare `latest` tag stays valid: the
 * wizard warns, it does not refuse.
 */
export function isImageRef(value: string): boolean {
  const ref = value.trim();
  if (ref === "" || ref.length > 255 || /\s/.test(ref)) {
    return false;
  }
  for (const ch of ref) {
    const code = ch.codePointAt(0) ?? 0;
    if (code <= 0x1f || code === 0x7f) {
      return false;
    }
  }
  const at = ref.indexOf("@");
  let repo = ref;
  if (at >= 0) {
    const digest = ref.slice(at + 1);
    if (digest === "" || digest.includes("@") || !/^sha256:[0-9a-fA-F]{64}$/.test(digest)) {
      return false;
    }
    repo = ref.slice(0, at);
  }
  const slash = repo.lastIndexOf("/");
  const colon = repo.lastIndexOf(":");
  let name = repo;
  if (colon >= 0 && colon > slash) {
    const tag = repo.slice(colon + 1);
    if (tag === "" || !/^[\w][\w.-]{0,127}$/.test(tag)) {
      return false;
    }
    name = repo.slice(0, colon);
  }
  if (name === "") {
    return false;
  }
  const headEnd = name.indexOf("/");
  if (headEnd >= 0) {
    const head = name.slice(0, headEnd);
    if (head.includes(".") || head.includes(":") || head.toLowerCase() === "localhost") {
      if (!isPublicRegistryHost(head)) {
        return false;
      }
      name = name.slice(headEnd + 1);
    }
  }
  if (name === "") {
    return false;
  }
  return name.split("/").every((component) => /^[a-z0-9]+(?:[._-][a-z0-9]+)*$/.test(component));
}

/**
 * isPublicRegistryHost accepts a registry host that is not the node-local
 * scope, mirroring checkRegistryHost (internal/deploy/image.go): bracketed
 * IPv6 literals, host:port with port 1-65535, plain IPs (loopback and
 * unspecified refused, including inet_aton shorthand), the localhost
 * spellings, or dot-separated RFC 1123 labels.
 */
export function isPublicRegistryHost(host: string): boolean {
  if (host.startsWith("[")) {
    const end = host.indexOf("]");
    if (end < 0) {
      return false;
    }
    const inner = host.slice(1, end);
    const kind = ipLiteralKind(inner);
    if (kind === null || kind === "loopback" || kind === "unspecified") {
      return false;
    }
    const rest = host.slice(end + 1);
    if (rest === "") {
      return true;
    }
    if (!rest.startsWith(":")) {
      return false;
    }
    return isRegistryPort(rest.slice(1));
  }
  if (host.includes(":")) {
    // Unbracketed IPv6 is not a valid registry host; a second colon is a
    // malformed host:port.
    if ((host.match(/:/g) ?? []).length !== 1) {
      return false;
    }
    const colon = host.indexOf(":");
    if (!isRegistryPort(host.slice(colon + 1))) {
      return false;
    }
    return isRegistryName(host.slice(0, colon));
  }
  return isRegistryName(host);
}

/** isRegistryPort accepts the Docker registry port range 1-65535. */
function isRegistryPort(port: string): boolean {
  if (!/^\d{1,5}$/.test(port)) {
    return false;
  }
  const value = Number(port);
  return value >= 1 && value <= 65535;
}

/**
 * isRegistryName validates a registry host without a port: an IP address
 * (loopback and unspecified refused, including inet_aton shorthand), the
 * localhost spellings, or dot-separated RFC 1123 labels.
 */
function isRegistryName(bare: string): boolean {
  if (bare === "") {
    return false;
  }
  const kind = ipLiteralKind(bare) ?? inetAtonKind(bare);
  if (kind === "loopback" || kind === "unspecified") {
    return false;
  }
  if (kind === "address") {
    return true;
  }
  const lower = bare.toLowerCase();
  const dotted = lower.endsWith(".") ? lower.slice(0, -1) : lower;
  if (dotted === "localhost" || dotted.endsWith(".localhost")) {
    return false;
  }
  if (lower.endsWith(".")) {
    return false;
  }
  return lower.split(".").every((label) => IMAGE_LABEL_PATTERN.test(label));
}

const IMAGE_LABEL_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/i;

type IpKind = "loopback" | "unspecified" | "address";

/**
 * ipLiteralKind classifies a strict IP literal (dotted-quad IPv4 or IPv6):
 * null when it is neither.
 */
function ipLiteralKind(host: string): IpKind | null {
  const v4 = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (v4) {
    const parts = v4.slice(1).map(Number);
    if (parts.some((part) => part > 255)) {
      return null;
    }
    if (parts[0] === 127) {
      return "loopback";
    }
    if (parts.every((part) => part === 0)) {
      return "unspecified";
    }
    return "address";
  }
  if (!isIpv6Address(host)) {
    return null;
  }
  if (host === "::1") {
    return "loopback";
  }
  if (host === "::") {
    return "unspecified";
  }
  return "address";
}

/** isIpv6Address accepts colon-separated hextets with one :: compression. */
function isIpv6Address(host: string): boolean {
  if (!/^[0-9a-fA-F:]+$/.test(host) || host.includes(":::")) {
    return false;
  }
  const halves = host.split("::");
  if (halves.length > 2) {
    return false;
  }
  const groups = (part: string): Array<string> => (part === "" ? [] : part.split(":"));
  const isHextet = (group: string): boolean => /^[0-9a-fA-F]{1,4}$/.test(group);
  if (halves.length === 1) {
    const all = groups(halves[0] ?? "");
    return all.length === 8 && all.every(isHextet);
  }
  const left = groups(halves[0] ?? "");
  const right = groups(halves[1] ?? "");
  return (
    left.every(isHextet) && right.every(isHextet) && left.length + right.length <= 7
  );
}

/**
 * inetAtonKind classifies classic dotted-decimal shorthand (a, a.b, a.b.c,
 * a.b.c.d, decimal parts only): loopback for 127/8, unspecified for 0,
 * address for any other value, null when it is not numeric shorthand.
 */
function inetAtonKind(host: string): IpKind | null {
  if (!/^[0-9.]+$/.test(host) || !host.includes(".")) {
    return null;
  }
  const parts = host.split(".");
  if (parts.length < 1 || parts.length > 4) {
    return null;
  }
  const nums: Array<number> = [];
  for (const part of parts) {
    if (part === "" || part.length > 10 || !/^\d+$/.test(part)) {
      return null;
    }
    const value = Number(part);
    if (!Number.isSafeInteger(value) || value > 0xffffffff) {
      return null;
    }
    nums.push(value);
  }
  let value: number;
  const get = (index: number): number => nums[index] ?? 0;
  switch (nums.length) {
    case 1:
      value = get(0);
      break;
    case 2:
      if (get(0) > 0xff || get(1) > 0xffffff) {
        return null;
      }
      value = get(0) * 0x1000000 + get(1);
      break;
    case 3:
      if (get(0) > 0xff || get(1) > 0xff || get(2) > 0xffff) {
        return null;
      }
      value = get(0) * 0x1000000 + get(1) * 0x10000 + get(2);
      break;
    default:
      if (nums.some((num) => num > 0xff)) {
        return null;
      }
      value = get(0) * 0x1000000 + get(1) * 0x10000 + get(2) * 0x100 + get(3);
      break;
  }
  const unsigned = value >>> 0;
  if (unsigned >>> 24 === 0x7f) {
    return "loopback";
  }
  // A zero value arises only from all-zero parts here.
  if (unsigned === 0) {
    return "unspecified";
  }
  return "address";
}

/**
 * isLatestImageTag reports a reference whose effective tag is `latest`
 * (explicit or implied): the wizard warns that redeploys follow the moving
 * tag, it does not block. A digest-pinned reference is frozen and never
 * warns, even when its tag reads `latest`.
 */
export function isLatestImageTag(value: string): boolean {
  const ref = value.trim();
  const at = ref.indexOf("@");
  if (at >= 0) {
    return false;
  }
  const slash = ref.lastIndexOf("/");
  const colon = ref.lastIndexOf(":");
  if (colon >= 0 && colon > slash) {
    return ref.slice(colon + 1).toLowerCase() === "latest";
  }
  return true;
}

/** imageRefSchema gates the image reference field. Gate-only. */
export const imageRefSchema = z.string().trim().min(1).refine(isImageRef);

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
