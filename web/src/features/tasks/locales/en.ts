/**
 * Tasks English catalog: the background-task progress card (JUS-91).
 * Only generated chrome is translated; streamed step text stays as sent.
 */
const en = {
  card: {
    title: "Background tasks",
    viewLogs: "View logs",
    dismiss: "Dismiss",
    collapse: "Collapse",
    expand: "Expand",
    status: {
      queued: "Queued",
      running: "Running",
      succeeded: "Succeeded",
      failed: "Failed",
      unknown: "Finished",
    },
    steps: {
      cloning: "Cloning",
      building: "Building",
      pushing: "Pushing",
      starting: "Starting",
    },
  },
};

export default en;

/** TasksMessages is the shape every tasks locale must satisfy. */
export type TasksMessages = typeof en;
