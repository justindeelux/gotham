const en = {
  page: {
    eyebrow: "Manage · Integrations",
    title: "Storages",
    description:
      "Destinations that backups write to. Configure one here and every backup run and schedule can use it.",
    add: "Add storage",
  },
  title: "Storage destinations",
  empty: "No storage destinations yet",
  onlyBuiltin:
    "Backups are kept on the control-plane disk. Add an S3-compatible destination to keep copies somewhere else.",
  builtin: "Built in",
  usage: "{count} backups · {size}",
  freeSpace: "{free} free of {total}",
  test: "Test",
  testFailed: "The connection test failed.",
  deleteConfirm:
    "Delete {name}? Backups stored there become unreadable and cannot be restored. A destination still used by a schedule cannot be deleted.",
  saved: "Storage “{name}” saved · credentials sealed",
  updated: "Storage “{name}” updated",
  deleted: "Storage “{name}” deleted",
  addTitle: "Add storage",
  editTitle: "Edit storage",
  save: "Save storage",
  localNote:
    "Local disk keeps artifacts on the control plane. It needs no setup and cannot be deleted.",
  secretsNote:
    "Secrets are sealed on the server and never shown back — leave the key fields blank to keep the stored ones.",
  kindS3: "S3-compatible",
  kindLocal: "Local disk",
  controlPlaneDisk: "control-plane disk",
  credsOk: "Credentials configured",
  fields: {
    name: "Name",
    kind: "Kind",
    endpoint: "Endpoint",
    region: "Region",
    bucket: "Bucket",
    prefix: "Prefix",
    accessKey: "Access key",
    secretKey: "Secret key",
  },
  placeholders: {
    name: "Storage name",
    endpoint: "https://…endpoint",
    region: "Region (e.g. auto)",
    bucket: "Bucket",
    prefix: "Key prefix (optional)",
    accessKey: "Access key",
    accessKeyEdit: "Access key · blank keeps stored keys",
    secretKey: "Secret key",
    secretKeyEdit: "Secret key · blank keeps stored keys",
  },
  aria: {
    name: "Storage name",
    kind: "Storage kind",
    endpoint: "Endpoint",
    region: "Region",
    bucket: "Bucket",
    prefix: "Key prefix",
    accessKey: "Access key",
    secretKey: "Secret key",
    test: "Test storage {name}",
    edit: "Edit storage {name}",
    delete: "Delete storage {name}",
  },
  errors: {
    invalidRequest: "The storage settings were refused",
    notFound: "Storage not found",
    conflict: "The storage cannot be changed",
    forbidden: "Only an owner with admin access can manage storages",
    unavailable: "Storage management is unavailable",
    agentUnreachable:
      "The node agent is unreachable or the job failed on the node. Check the node status and retry.",
  },
  validation: {
    nameRequired: "Enter a storage name.",
    s3LocationRequired:
      "An S3-compatible storage needs an endpoint and a bucket.",
    s3KeysRequired: "Enter both the access key and the secret key.",
  },
};

export default en;

export type StoragesMessages = typeof en;
