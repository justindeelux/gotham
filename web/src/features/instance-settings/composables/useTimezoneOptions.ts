import type { SelectOption } from "naive-ui";

function offsetLabel(zone: string, now: Date): string {
  try {
    const part = new Intl.DateTimeFormat("en", { timeZone: zone, timeZoneName: "longOffset" })
      .formatToParts(now)
      .find((p) => p.type === "timeZoneName")?.value;
    return part && part !== "GMT" ? part.replace("GMT", "UTC") : "UTC+00:00";
  } catch {
    return "";
  }
}

/** IANA zones (plus UTC and the saved value) with their current UTC offset. */
export function timezoneOptions(current: string): SelectOption[] {
  const now = new Date();
  const zones = new Set<string>(["UTC", ...Intl.supportedValuesOf("timeZone")]);
  if (current) {
    zones.add(current);
  }
  return [...zones].map((zone) => ({ value: zone, label: `${zone} (${offsetLabel(zone, now)})` }));
}
