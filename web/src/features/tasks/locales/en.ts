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

export type TasksMessages = typeof en;
