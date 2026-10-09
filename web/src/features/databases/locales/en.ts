/**
 * Databases English catalog. Keys are semantic whole sentences with named
 * parameters; raw technical values (engine names, versions, DSNs, cron
 * expressions, bucket names, paths, dumps, server diagnostics) are
 * interpolated, never translated.
 */
const en = {
  validation: {
    nameRule: "Name must be 1-63 characters of letters, digits, ., _ or -.",
    cronRequired: "Cron expression is required, e.g. 0 2 * * *.",
    targetNameRequired: "Target name is required.",
    s3LocationRequired: "Endpoint and bucket are required for an S3 target.",
    s3KeysRequired:
      "Access key and secret key are required for a new S3 target.",
  },
  status: {
    creating: "Creating",
    running: "Running",
    stopped: "Stopped",
    error: "Error",
    deleting: "Deleting",
  },
  errors: {
    withDetail: "{summary} ({detail})",
    requestRefused: "Request refused",
    databaseNotFound:
      "Database not found. It may have been deleted or belong to another account.",
    nameTaken: "A database with that name already exists.",
    databaseAgentUnreachable:
      "The node agent is unreachable or the healthcheck failed. Check the node status and retry.",
    featureDisabled:
      "Databases are disabled on the control plane (FEATURE_DATABASES=false).",
    backupInvalidRequest:
      "Invalid request. Check the cron expression and target fields.",
    sessionExpired: "Session expired. Please sign in again.",
    backupNotFound:
      "Not found. It may have been deleted or belong to another account.",
    backupConflict:
      "A backup or restore is already running for this database.",
    backupAgentUnreachable:
      "The node agent is unreachable or the job failed on the node. Check the node status and retry.",
  },
  detail: {
    breadcrumb: "Breadcrumb",
    tabs: {
      overview: "Overview",
      backups: "Backups",
      settings: "Settings",
    },
    noSelection: "No database selected.",
    serverPinnedReason:
      "A database cannot change server once created; only moving it to another environment is possible.",
    actions: {
      start: "Start",
      stop: "Stop",
      restart: "Restart",
      rename: "Rename",
    },
    deleteConfirm:
      "Delete this database? The container is removed from the node, the volume {volume} is kept for 7 days before permanent removal.",
    overview: {
      details: "Details",
      engine: "Engine",
      node: "Node",
      publicPort: "Public port",
      publicPortOff: "off · internal network only",
      volume: "Volume",
      container: "Container",
      created: "Created",
    },
    credentials: {
      title: "Credentials",
      storedNote: "Stored encrypted · owner only",
      username: "Username",
      password: "Password",
      database: "Database",
      rootPassword: "Root password",
      copy: "Copy",
      reveal: "Reveal",
      hide: "Hide",
      connectionString: "Connection string",
      copyConnection: "Copy connection string",
      nodeUnknown: "Public endpoint unavailable · node address unknown.",
      empty:
        "No credentials cached — they load automatically with the page.",
      noCached: "No {label} cached yet",
      nodeDsnError: "Node address unknown — cannot build the public DSN yet",
      notLoaded: "Credentials are not loaded yet",
    },
    rename: {
      title: "Rename database",
      hint: "Only the display name changes — the container, volume and credentials stay untouched.",
      placeholder: "New database name",
    },
    lifecycle: {
      started: "Database started",
      stopped: "Database stopped",
      restarted: "Database restarted",
      renamed: "Database renamed to “{name}”",
      deleted: "Database “{name}” deleted · volume kept for 7 days",
      locationSaved: "Location saved",
    },
  },
  backups: {
    runs: {
      title: "Backups",
      destination: "Destination",
      destinationAria: "Backup destination",
      refresh: "Refresh",
      backupNow: "Backup now",
      backupConfirm:
        "A backup stops this database while the dump runs, so it is briefly unavailable. Continue?",
      noTarget:
        "No S3 target configured — backups are stored on the control plane disk. Add an S3-compatible target below to keep them off-node.",
      sizePending: "size pending",
      dumpInProgress: "Dump in progress…",
      scheduled: "scheduled",
      restore: "Restore",
      restoreAria: "Restore backup from {when}",
      deleteAria: "Delete backup from {when}",
      deleteConfirm:
        "Delete this backup? The stored artifact goes first, then the row. This cannot be undone.",
      empty: "No backups yet",
      emptyHint:
        "Queue a manual backup above, or add a schedule so the control plane dumps this database automatically.",
      queued: "Backup queued · the dump runs in a temporary container",
      deleted: "Backup deleted",
    },
    runStatus: {
      running: "running",
      completed: "completed",
      failed: "failed",
    },
    runType: {
      manual: "manual",
      scheduled: "scheduled",
    },
    restores: {
      title: "Restore history",
      subtitle: "Durable result of each queued restore",
      backupRef: "backup {id}",
      finished: "finished",
      completed: "Restore completed",
      failed: "Restore failed",
      failedWithDetail: "Restore failed: {detail}",
      queued: "Restore queued · the database is stopped while it runs",
    },
    dialog: {
      title: "Restore database",
      prompt:
        "Restore {name} from the backup {backup} ({when}, {size})?",
      warning:
        "The restore runs in a temporary container and overwrites the current data of this database. This cannot be undone — back up first if the live data still matters.",
      confirm: "Restore · overwrite data",
    },
    schedules: {
      title: "Schedules",
      subtitle: "Cron in the control plane",
      nextRun: "Next run",
      lastRun: "last",
      on: "On",
      off: "Off",
      enableAria: "Enable schedule {cron}",
      editAria: "Edit schedule {cron}",
      deleteAria: "Delete schedule {cron}",
      deleteConfirm: "Delete this schedule? Past backups stay untouched.",
      empty: "No schedules yet — automatic backups are off",
      newTitle: "New schedule",
      editTitle: "Edit schedule",
      cronAria: "Cron expression",
      destinationAria: "Schedule destination",
      enableNewAria: "Enable the new schedule",
      add: "Add schedule",
      save: "Save schedule",
      saved: "Schedule saved · next run computed from {cron}",
      updated: "Schedule updated · next run computed from {cron}",
      enabled: "Schedule enabled",
      paused: "Schedule paused",
      deleted: "Schedule deleted",
    },
    targets: {
      title: "Backup targets",
      subtitle: "S3-compatible storage",
      kindS3: "S3-compatible",
      kindLocal: "Local disk",
      localDefault: "Local disk (default)",
      localDisk: "local disk",
      deletedTarget: "deleted target",
      controlPlaneDisk: "control plane disk",
      credsOk: "Credentials configured",
      credsMissing: "No credentials",
      test: "Test",
      testAria: "Test target {name}",
      editAria: "Edit target {name}",
      deleteAria: "Delete target {name}",
      deleteConfirm:
        "Delete target “{name}”? Past backups keep their location but can no longer be read back from this target.",
      empty: "No backup targets yet",
      emptyHint:
        "Backups fall back to the control plane disk until an S3-compatible target is configured.",
      newTitle: "New target",
      editTitle: "Edit target",
      namePlaceholder: "Target name",
      nameAria: "Target name",
      kindAria: "Target kind",
      endpointPlaceholder: "https://…endpoint",
      endpointAria: "Endpoint",
      regionPlaceholder: "Region (e.g. auto)",
      regionAria: "Region",
      bucketPlaceholder: "Bucket",
      bucketAria: "Bucket",
      prefixPlaceholder: "Key prefix (optional)",
      prefixAria: "Key prefix",
      accessKeyPlaceholder: "Access key",
      accessKeyEditPlaceholder: "Access key · blank keeps stored keys",
      accessKeyAria: "Access key",
      secretKeyPlaceholder: "Secret key",
      secretKeyEditPlaceholder: "Secret key · blank keeps stored keys",
      secretKeyAria: "Secret key",
      secretsNote:
        "Secrets are sealed on the server and never shown back — leave the key fields blank to keep the stored ones.",
      add: "Add target",
      save: "Save target",
      saved: "Target “{name}” saved · credentials sealed",
      updated: "Target “{name}” updated",
      deleted: "Target “{name}” deleted",
      testFailed: "Test failed",
    },
  },
  wizard: {
    title: "Create database",
    stepOf: "Step {current} of {total} · {step}.",
    intro:
      "Each database is a container with its own volume on one node; the credentials are generated server-side and stored encrypted.",
    steps: {
      engine: "Engine",
      configure: "Configure",
      review: "Review",
    },
    engineStep: {
      engine: "Engine",
      preselected: "Engine: {engine}",
      change: "Change",
      version: "Version",
      defaultVersion: "Default: {version}",
      node: "Node",
      engineMeta: "· {repo}:{version} · port {port}",
    },
    configure: {
      name: "Name",
      nameFeedback:
        "Used for the container and the credentials; 1-63 chars: letters, digits, ., _ or -.",
      namePlaceholder: "pg-orders",
      expose: "Expose a public port",
      exposeWarning:
        "A public port is an attack surface and cannot change later — Docker port bindings are fixed at creation. Leave it off unless an external client requires it.",
      publicPort: "Public port",
      publicPortFeedback: "Host port forwarding to the engine port.",
      publicPortPlaceholder: "e.g. 15432",
    },
    review: {
      intro: "Review the database before creating it:",
      engineImage: "Engine / image · {image}",
      node: "Node · {server}",
      name: "Name · {name}",
      publicPort: "Public port · {mapping}",
      publicPortOff: "off · internal network only",
      volume: "Volume · kept 7 days after deletion",
      successTitle: "Database created",
      successBody:
        "Container provisioning started on the node. Save these credentials — they stay available on the database detail page.",
    },
    back: "Back",
    continue: "Continue",
    create: "Create database",
    done: "Done",
    scopeError: "Select a project and environment first.",
    created: "Database “{name}” created",
  },
};

export default en;
