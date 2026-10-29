---
title: Backups
---

# Backups

The **Backups** page is the product-wide home for data protection. It lists every
backup run and schedule across the resources you can access, and it is the only
place the owner manages storage destinations. A database page still has its own
**Backups** tab for that database; anything that applies to all resources lives
here.

## Backups tab

The list shows one row per run:

- **Resource** — the application, database or service and a link to its page.
- **Status** — running, completed or failed.
- **Size** — the stored artifact size, or `size pending` while the run works.
- **Storage** — the provider that received the artifact.
- **Started** and **Duration** — timestamps of the run.

Use the type and status filters to narrow the list. **Backup now** asks for a
resource and a storage provider, then queues a manual run. **Restore** on a
completed run queues a restore and asks for confirmation first; **Delete**
removes the stored artifact and its row.

Below the runs, **Schedules** lists every automatic backup with its cadence in
plain language, its resource and storage provider, the last and next run, and an
enabled switch. **New schedule** picks a resource, a storage provider and one of
the presets.

Applications, services and databases share the same page and run list. A run
belongs to whichever resource it captured and resolves to that resource's page.

### Downtime

A database backup stops the database while the dump runs, then starts it again
if it was running. A volume archive does the same for an application or service.
A schedule can opt out with **Do not stop the resource**: there is no downtime,
but files written during the archive may be inconsistent, and the history labels
the run. For a busy resource, pick a quiet window instead.

### Restoring

A restore overwrites the current data of the resource. It cannot be undone, so
take a fresh backup first if the live data still matters. Restore history stays
on the resource tab so a failed restore is visible with its cause.

## Storage tab

The **Storage** tab is visible to the owner only. It is an owner-level
catalogue: configure a destination once and use it from every schedule and
manual run.

Every install ships a built-in **Local disk** provider, which writes to the
control plane's backup directory. It cannot be deleted, so a fresh install can
back up with no setup.

S3-compatible storage works with AWS S3, Cloudflare R2, MinIO and similar
services. A provider has a name, endpoint, region, bucket, optional key prefix,
access key and secret key. The keys are sealed on the server and never shown
back; leaving them blank on an edit keeps the stored ones. **Test** checks the
bucket and credentials, or the local directory's writability, and shows the
result inline.

Deleting a provider does not delete past backups, but they can no longer be read
back from it, so restores from those runs fail.

## Members

A member sees only runs and schedules of resources in projects they belong to.
Provider names appear in the run list, but providers cannot be chosen, read or
edited. Storage management is owner-only.
