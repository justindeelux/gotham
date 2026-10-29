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
back from it, so restores from those runs fail and the runs stay listed as
**Provider removed**. A provider still used by a schedule cannot be deleted:
move or delete those schedules first, so no schedule silently falls back to the
local disk.

## Retention and cleanup

Each schedule has **Keep the last scheduled backups** (7 for a new schedule).
After a scheduled run succeeds, older completed runs of that schedule beyond the
limit are deleted oldest first, artifact included.

What is never deleted automatically:

- manual backups — only you delete those;
- the newest completed scheduled run;
- a run that is still executing;
- failed runs younger than 30 days.

A schedule created before this version keeps every backup until you set a limit;
the schedule list says **Keeps every scheduled backup** in that case.

If the provider is unreachable while a run is being removed, the run stays in
**Cleanup pending** with the last error. New backups keep running. The hourly
sweep retries in the background; **Retry cleanup** attempts immediately and
**Discard** forgets the row without deleting the artifact.

When an application, service or database is deleted permanently, its backup
artifacts are queued for deletion before its rows disappear. A database still
keeps its soft-delete grace window first, so nothing is removed early. The queue
survives a provider outage and is drained by the same sweeper.

The Storage tab shows, per provider, how many backups it holds and their total
size, and for Local disk the free space. Before a run goes to Local disk, the
control plane checks that the free space covers the size of the previous run of
that resource plus 20% (or 64 MiB when there is no previous run) and refuses the
run with a plain message instead of filling the disk.

## Members

A member sees only runs and schedules of resources in projects they belong to.
Provider names appear in the run list, but providers cannot be chosen, read or
edited. Storage management is owner-only.
