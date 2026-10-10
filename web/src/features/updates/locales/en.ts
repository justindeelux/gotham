/**
 * English updates catalog (namespace `updates`): Settings → Updates page,
 * schedule form and validation messages. Only values are translated in
 * `vi.ts`; keys and params stay identical.
 */
const en = {
  page: {
    eyebrow: "Settings · System",
    title: "Updates",
    description:
      "Check for new Gotham releases, update the control plane and choose when Gotham checks and applies updates automatically.",
    unavailable: "Self-update is turned off on this installation.",
  },
  actions: {
    check: "Check for updates",
    update: "Update now",
    save: "Save schedule",
    reset: "Reset",
    cancel: "Cancel",
  },
  stats: {
    current: "Current version",
    latest: "Latest available",
    lastChecked: "Last checked",
    nextCheck: "Next check {time}",
    upToDate: "Up to date",
    updateAvailable: "Update available",
    notChecked: "not checked yet",
    unknown: "unknown",
    channel: { stable: "stable", beta: "beta" },
  },
  notes: { title: "Release notes · {version}", empty: "This release has no notes." },
  confirm: {
    title: "Update Gotham to {version}?",
    body: "Gotham downloads and verifies the release, then restarts. The dashboard is unavailable for a short time and reconnects automatically. A failed health check restores the previous version.",
  },
  progress: {
    title: "Update in progress",
    installing: "Installing {version}",
    restarting: "Restarting — this page updates automatically.",
  },
  last: {
    title: "Last update",
    ok: "Succeeded",
    staged: "In progress",
    rolled_back: "Rolled back",
    rollback_failed: "Rollback failed",
    no_backup: "Failed",
    wrapper_failed: "Failed",
    resuming: "Resuming",
    detail: "Detail: {detail}",
  },
  errors: {
    check: "Could not check for updates: {message}",
    apply: "The update failed: {message}",
    save: "Could not save the schedule: {message}",
    backoff:
      "Automatic updates skip {version} until {time} after a failed update. Update now can still retry.",
  },
  schedule: {
    title: "Update schedule",
    hint: "Saved changes apply immediately — no restart.",
    timezone: "Times use {timezone}.",
    adminOnly: "Only platform administrators can check, update or change the schedule.",
    checkEnabled: "Check for updates automatically",
    checkHint: "Gotham looks for a new release on the schedule below.",
    autoApply: "Apply updates automatically",
    autoApplyHint: "Install a found release without confirmation. Off by default.",
    channel: "Channel",
    frequency: "Frequency",
    frequencies: { interval: "Every N hours", daily: "Daily", weekly: "Weekly" },
    intervalHours: "Interval (hours)",
    time: "Time",
    weekday: "Day of week",
    weekdays: {
      "0": "Sunday",
      "1": "Monday",
      "2": "Tuesday",
      "3": "Wednesday",
      "4": "Thursday",
      "5": "Friday",
      "6": "Saturday",
    },
    saved: "Schedule saved.",
  },
  validation: {
    intervalRange: "Enter a whole number of hours between 1 and 720.",
    timeFormat: "Use the HH:MM 24-hour format.",
    autoApplyNeedsCheck: "Automatic apply requires automatic checks.",
  },
};

export type UpdatesMessages = typeof en;
export default en;
