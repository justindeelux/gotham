import { z } from "zod";

export const applicationMessages = {
  domain: "applications.validation.domain",
  domainPort: "applications.validation.domainPort",
  domainService: "applications.validation.domainService",
} as const;

const NAME_PATTERN = /^[a-z][a-z0-9-]{2,30}$/;
const HOST_PATTERN = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;

export const appNameSchema = z.string().trim().regex(NAME_PATTERN);

export const branchSchema = z.string().trim().min(1);

export const providerSchema = z.string().min(1);

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

export function sourceTypeImplemented(value: string): boolean {
  return (
    value === "git_public" ||
    value === "git_private" ||
    value === "github_app" ||
    value === "gitlab_app" ||
    value === "dockerfile" ||
    value === "image" ||
    value === "compose"
  );
}

export const maxDockerfileBytes = 64 * 1024;

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

export const buildArgKeySchema = z
  .string()
  .trim()
  .min(1)
  .max(128)
  .refine((value) => !/[= \t\r\n\0]/.test(value));

export const maxBuildArgValueBytes = 4 * 1024;

export const buildArgValueSchema = z
  .string()
  .refine((value) => new TextEncoder().encode(value).length <= maxBuildArgValueBytes)
  .refine((value) => !value.includes("\0"));

export const cloneUrlSchema = z.string().trim().min(1);

export function isPublicGitUrl(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed === "" || /\s/.test(trimmed)) {
    return false;
  }
  for (const ch of trimmed) {
    const code = ch.codePointAt(0) ?? 0;
    if (code <= 0x1f || code === 0x7f) {
      return false;
    }
  }
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

export const publicCloneUrlSchema = z
  .string()
  .trim()
  .min(1)
  .refine(isPublicGitUrl);

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

function isRegistryPort(port: string): boolean {
  if (!/^\d{1,5}$/.test(port)) {
    return false;
  }
  const value = Number(port);
  return value >= 1 && value <= 65535;
}

function isRegistryName(bare: string): boolean {
  if (bare === "") {
    return false;
  }
  const kind = ipLiteralKind(bare) ?? numericDotsKind(bare);
  if (kind === "invalid") {
    return false;
  }
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
  return ipv6Kind(host);
}

function ipv6Kind(host: string): IpKind {
  const expanded = expandIpv6(host) ?? [];
  if (expanded.every((value) => value === 0)) {
    return "unspecified";
  }
  if (expanded.slice(0, 7).every((value) => value === 0) && expanded[7] === 1) {
    return "loopback";
  }
  if (expanded.slice(0, 5).every((value) => value === 0) && expanded[5] === 0xffff) {
    const low = [(expanded[6] ?? 0) >>> 8 & 0xff, (expanded[6] ?? 0) & 0xff, (expanded[7] ?? 0) >>> 8 & 0xff, (expanded[7] ?? 0) & 0xff];
    if (low[0] === 127) {
      return "loopback";
    }
    if (low.every((value) => value === 0)) {
      return "unspecified";
    }
  }
  return "address";
}

function expandIpv6(host: string): Array<number> | null {
  let head = host;
  let tail: Array<number> = [];
  const lastColon = host.lastIndexOf(":");
  const maybeV4 = host.slice(lastColon + 1);
  if (maybeV4.includes(".")) {
    const v4 = maybeV4.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
    if (!v4) {
      return null;
    }
    const parts = v4.slice(1).map(Number);
    if (parts.some((part) => part > 255)) {
      return null;
    }
    tail = [(parts[0] ?? 0) * 256 + (parts[1] ?? 0), (parts[2] ?? 0) * 256 + (parts[3] ?? 0)];
    head = host.slice(0, lastColon);
  }
  if (!/^[0-9a-fA-F:]*$/.test(head) || head.includes(":::")) {
    return null;
  }
  const halves = head.split("::");
  if (halves.length > 2) {
    return null;
  }
  const isHextet = (group: string): boolean => /^[0-9a-fA-F]{1,4}$/.test(group);
  const groups = (part: string): Array<string> => (part === "" ? [] : part.split(":"));
  const left = groups(halves[0] ?? "");
  const right = halves.length === 2 ? groups(halves[1] ?? "") : [];
  if (!left.every(isHextet) || !right.every(isHextet)) {
    return null;
  }
  const total = left.length + right.length + tail.length;
  if (halves.length === 1 ? total !== 8 : total >= 8) {
    return null;
  }
  const zeros = new Array<number>(8 - total).fill(0);
  const parse = (group: string): number => Number.parseInt(group, 16);
  return [...left.map(parse), ...zeros, ...right.map(parse), ...tail];
}

function isIpv6Address(host: string): boolean {
  return expandIpv6(host) !== null;
}

function numericDotsKind(host: string): IpKind | "invalid" | null {
  if (!/^[0-9.]+$/.test(host) || !host.includes(".")) {
    return null;
  }
  const parts = host.split(".");
  if (parts.length < 1 || parts.length > 4) {
    return "invalid";
  }
  const nums: Array<number> = [];
  for (const part of parts) {
    if (part === "" || part.length > 10 || !/^\d+$/.test(part)) {
      return "invalid";
    }
    const value = Number(part);
    if (!Number.isSafeInteger(value) || value > 0xffffffff) {
      return "invalid";
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
        return "invalid";
      }
      value = get(0) * 0x1000000 + get(1);
      break;
    case 3:
      if (get(0) > 0xff || get(1) > 0xff || get(2) > 0xffff) {
        return "invalid";
      }
      value = get(0) * 0x1000000 + get(1) * 0x10000 + get(2);
      break;
    default:
      if (nums.some((num) => num > 0xff)) {
        return "invalid";
      }
      value = get(0) * 0x1000000 + get(1) * 0x10000 + get(2) * 0x100 + get(3);
      break;
  }
  const unsigned = value >>> 0;
  if (unsigned >>> 24 === 0x7f) {
    return "loopback";
  }
  if (unsigned === 0) {
    return "unspecified";
  }
  return "address";
}

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

export const imageRefSchema = z.string().trim().min(1).refine(isImageRef);

export function isPrivateGitUrl(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed === "" || /\s/.test(trimmed)) {
    return false;
  }
  for (const ch of trimmed) {
    const code = ch.codePointAt(0) ?? 0;
    if (code <= 0x1f || code === 0x7f) {
      return false;
    }
  }
  if (!trimmed.includes("://")) {
    if (trimmed.startsWith("/")) {
      return false;
    }
    return isScpLikeUrl(trimmed);
  }
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
  if (scheme === "http:" || scheme === "https:") {
    if (parsed.hostname === "") {
      return false;
    }
    return parsed.username === "" && parsed.password === "";
  }
  if (scheme === "ssh:") {
    return parsed.hostname !== "" && parsed.password === "";
  }
  return false;
}

export function isScpLikeUrl(value: string): boolean {
  if (value.startsWith("git@")) {
    return true;
  }
  const at = value.indexOf("@");
  if (at <= 0) {
    return false;
  }
  const colon = value.indexOf(":");
  const slash = value.indexOf("/");
  return colon > at && (slash === -1 || slash > at);
}

export const privateCloneUrlSchema = z
  .string()
  .trim()
  .min(1)
  .refine(isPrivateGitUrl);

export function isHttpsGitUrl(value: string): boolean {
  const sep = value.trim().indexOf("://");
  if (sep < 0) {
    return false;
  }
  return value.trim().slice(0, sep).toLowerCase() === "https";
}

export const repoSchema = z.string().min(1);
export const serverSchema = z.string().min(1);

export const domainPortSchema = z
  .number({
    error: "applications.validation.domainPort",
  })
  .int("applications.validation.domainPort")
  .min(1, "applications.validation.domainPort")
  .max(65535, "applications.validation.domainPort");

export const domainProtocolSchema = z.enum(["http", "https"]);

export const domainServiceSchema = z
  .string()
  .trim()
  .refine(
    (value) => value === "" || COMPOSE_SERVICE_PATTERN.test(value),
    { message: "applications.validation.domainService" },
  );

export const hostDomainSchema = z.string().refine(
  (value) => value === "" || (value.length <= 253 && HOST_PATTERN.test(value)),
  { message: applicationMessages.domain },
);

export const MAX_COMPOSE_BYTES = 256 * 1024;

export const MAX_COMPOSE_FILE_PATH = 256;

export const COMPOSE_SERVICE_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

export const composeModeSchema = z.enum(["paste", "repo"]);

export type ComposeMode = z.infer<typeof composeModeSchema>;

export const composeContentSchema = z
  .string()
  .trim()
  .min(1)
  .refine((value) => new TextEncoder().encode(value).length <= MAX_COMPOSE_BYTES)
  .refine((value) => /^services\s*:/m.test(value));

export const composeServiceSchema = z
  .string()
  .trim()
  .min(1)
  .regex(COMPOSE_SERVICE_PATTERN);

export const composeFileSchema = z
  .string()
  .trim()
  .min(1)
  .refine((value) => new TextEncoder().encode(value).length <= MAX_COMPOSE_FILE_PATH)
  .refine((value) => !value.startsWith("/") && value !== "." && value !== "..")
  .refine((value) => !/(^|\/)\.\.(\/|$)/.test(value));
