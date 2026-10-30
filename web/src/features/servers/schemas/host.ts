import { z } from "zod";

const m = (key: string): string => `servers.settings.host.validation.${key}`;

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

export const dnsListSchema = z.string().refine((value) => {
  const items = splitList(value);
  return items.length <= 3 && new Set(items).size === items.length && items.every(isIP);
}, m("dns"));

export const dnsServerSchema = z
  .string()
  .trim()
  .refine((value) => value === "" || isIP(value), m("dnsSingle"));

export const dnsPrimaryServerSchema = z
  .string()
  .trim()
  .refine(isIP, m("dnsSingle"));

export function dnsServersValid(list: string[]): boolean {
  const items = list.map((item) => item.trim()).filter((item) => item !== "");
  return items.length <= 3 && new Set(items).size === items.length && items.every(isIP);
}

export interface InterfaceConfig {
  ipv4: { mode: string; address: string; gateway: string };
  ipv6: { enabled: boolean; mode: string; address: string; gateway: string };
}

export function sameInterface(a: InterfaceConfig, b: InterfaceConfig): boolean {
  return (
    a.ipv4.mode === b.ipv4.mode &&
    a.ipv4.address === b.ipv4.address &&
    a.ipv4.gateway === b.ipv4.gateway &&
    a.ipv6.enabled === b.ipv6.enabled &&
    a.ipv6.mode === b.ipv6.mode &&
    a.ipv6.address === b.ipv6.address &&
    a.ipv6.gateway === b.ipv6.gateway
  );
}

export function isRiskyInterfaceChange(prev: InterfaceConfig, next: InterfaceConfig): boolean {
  if (prev.ipv4.mode === "static" && prev.ipv4.address !== "") {
    if (
      next.ipv4.mode !== "static" ||
      next.ipv4.address !== prev.ipv4.address ||
      next.ipv4.gateway !== prev.ipv4.gateway
    ) {
      return true;
    }
  }
  if (prev.ipv6.enabled && prev.ipv6.mode === "static" && prev.ipv6.address !== "") {
    if (
      !next.ipv6.enabled ||
      next.ipv6.mode !== "static" ||
      next.ipv6.address !== prev.ipv6.address ||
      next.ipv6.gateway !== prev.ipv6.gateway
    ) {
      return true;
    }
  }
  return false;
}

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

const systemSchema = z.object({
  hostname: z.string(),
  ntp_enabled: z.boolean(),
  ntp_servers: z.array(z.string()),
  timezone: z.string(),
});

export const nodeHostEnvelopeSchema = z.object({
  host: z.object({
    capability: z.enum([
      "ready",
      "unsupported_stack",
      "helper_missing",
      "helper_denied",
      "agent_outdated",
      "offline",
    ]),
    reason: z.string().optional(),
    repair: z.boolean(),
    network: networkSchema,
    system: systemSchema,
    pending: z
      .object({ previous: networkSchema, proposed: networkSchema, deadline: z.string() })
      .nullable(),
  }),
});

export const nodeHelperEnvelopeSchema = z.object({
  host: nodeHostEnvelopeSchema.shape.host,
  log: z.string().optional().default(""),
});
