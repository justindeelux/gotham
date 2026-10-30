export interface CronPreset {
  key: "daily" | "weekly";
  cron: string;
}

// The same cron presets the schedule modal offers. Daily at 03:00, weekly on
// Sunday at 03:00 (instance local time on the control plane).
export const cronPresets: CronPreset[] = [
  { key: "daily", cron: "0 3 * * *" },
  { key: "weekly", cron: "0 3 * * 0" },
];

export function cronPresetKey(cron: string): "daily" | "weekly" | "custom" {
  const normalized = cron.trim();
  return cronPresets.find((preset) => preset.cron === normalized)?.key ?? "custom";
}
