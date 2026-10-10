import { z } from "zod";

/**
 * Instance-settings schemas. Field messages are namespaced i18n keys resolved
 * at display time (see shared/validation/naiveAdapter). They mirror the
 * server-side validation in internal/instance/validate.go, which stays the
 * source of truth: the server re-validates and answers field errors.
 */
const m = (key: string): string => `instance-settings.validation.${key}`;

const v4 = /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/;

export function isIPv4(value: string): boolean {
  return v4.test(value);
}

export function isIPv6(value: string): boolean {
  if (!/^[0-9a-fA-F:]{2,39}$/.test(value) || !value.includes(":")) {
    return false;
  }
  try {
    return new URL(`http://[${value}]/`).hostname !== "";
  } catch {
    return false;
  }
}

const isIP = (value: string): boolean => isIPv4(value) || isIPv6(value);

/** splitList turns "a, b" input into trimmed non-empty entries. */
export function splitList(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item !== "");
}

const hostLabel = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/;

export function isHostname(value: string): boolean {
  return value.length <= 253 && value.split(".").every((label) => hostLabel.test(label));
}

export const controlPlaneUrlSchema = z
  .string()
  .trim()
  .refine((value) => {
    if (value === "") {
      return true;
    }
    try {
      const url = new URL(value);
      return (
        (url.protocol === "http:" || url.protocol === "https:") &&
        url.hostname !== "" &&
        url.username === "" &&
        url.password === "" &&
        url.search === "" &&
        url.hash === ""
      );
    } catch {
      return false;
    }
  }, m("url"));

export const instanceNameSchema = z
  .string()
  .trim()
  .min(1, m("name"))
  .max(64, m("name"));

export const timezoneSchema = z.string().trim().refine((value) => {
  if (value === "" || value === "Local") {
    return false;
  }
  try {
    new Intl.DateTimeFormat("en", { timeZone: value });
    return true;
  } catch {
    return false;
  }
}, m("timezone"));

/** dnsListSchema validates the comma separated resolver text. */
export const dnsListSchema = z.string().refine((value) => {
  const items = splitList(value);
  return items.length <= 3 && new Set(items).size === items.length && items.every(isIP);
}, m("dns"));

export const ipv4AddressSchema = z
  .string()
  .trim()
  .refine((value) => {
    const [address, bits, ...rest] = value.split("/");
    return (
      rest.length === 0 && isIPv4(address ?? "") && /^([1-9]|[12]\d|3[0-2])$/.test(bits ?? "")
    );
  }, m("ipv4Address"));

export const ipv4GatewaySchema = z.string().trim().refine(isIPv4, m("ipv4Gateway"));

export const ipv6AddressSchema = z
  .string()
  .trim()
  .refine((value) => {
    const [address, bits, ...rest] = value.split("/");
    const n = Number(bits);
    return rest.length === 0 && isIPv6(address ?? "") && Number.isInteger(n) && n >= 1 && n <= 128;
  }, m("ipv6Address"));

export const ipv6GatewaySchema = z.string().trim().refine(isIPv6, m("ipv6Gateway"));

export const hostnameSchema = z
  .string()
  .trim()
  .refine((value) => value === "" || isHostname(value), m("hostname"));

export const ntpListSchema = z.string().refine((value) => {
  const items = splitList(value);
  return (
    items.length <= 4 &&
    new Set(items.map((item) => item.toLowerCase())).size === items.length &&
    items.every((item) => isIP(item) || isHostname(item))
  );
}, m("ntp"));

const settingFieldSchema = z.object({
  value: z.string(),
  source: z.enum(["env", "db", "default"]),
  locked: z.boolean(),
});

const networkSchema = z.object({
  dns_servers: z.array(z.string()),
  ipv4: z.object({ mode: z.string(), address: z.string(), gateway: z.string() }),
  ipv6: z.object({
    enabled: z.boolean(),
    mode: z.string(),
    address: z.string(),
    gateway: z.string(),
  }),
});

/** stateEnvelopeSchema checks the {settings} response envelope. */
export const stateEnvelopeSchema = z.object({
  settings: z.object({
    general: z.object({
      control_plane_url: settingFieldSchema,
      instance_name: settingFieldSchema,
      timezone: settingFieldSchema,
    }),
    network: networkSchema,
    system: z.object({
      hostname: z.string(),
      ntp_enabled: z.boolean(),
      ntp_servers: z.array(z.string()),
    }),
    capabilities: z.object({ network: z.boolean(), system: z.boolean() }),
    pending: z
      .object({ previous: networkSchema, proposed: networkSchema, deadline: z.string() })
      .nullable(),
  }),
});
