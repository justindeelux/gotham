---
title: Storages
---

# Storages

**Manage → Integrations → Storages** is the home of the destinations backups
write to. It is owner-only, like the other operator pages: an API token also
needs the `admin` scope.

Configure a destination once here and every manual run and schedule can pick it.
The Backups page no longer has its own storage tab; when no destination of your
own is configured it links here.

## Built-in Local disk

Every install ships a built-in **Local disk** destination, which writes to the
control plane's backup directory. It needs no setup and cannot be deleted or
renamed, so a fresh install can back up immediately.

The list shows, per destination, how many backups it holds and their total
size. For Local disk it also shows the free space.

## S3-compatible storage

S3-compatible storage works with AWS S3, Cloudflare R2, MinIO and similar
services. A destination has a name, endpoint, region, bucket, optional key
prefix, access key and secret key.

- **Secrets are write-only.** They are sealed on the server and never returned
  by the API or shown in the UI. Leaving the key fields blank on an edit keeps
  the stored ones.
- **Test** writes, reads back and deletes a small probe object, so a destination
  that is read-only or blocks deletes is reported before the first real backup.
  For Local disk it proves the same round trip. The result is shown inline.

## Deleting a destination

Deleting a destination does not delete past backups, but they can no longer be
read back from it, so restores from those runs fail and the runs stay listed as
**Provider removed**. The UI asks for confirmation before that happens.

A destination still used by a schedule cannot be deleted: move or delete those
schedules first, so no schedule silently falls back to the local disk. Deleting
a storage that still holds backups without confirmation is rejected with a
clear conflict error.

## API

The standalone storage API is under `/api/v1/storages` and needs the owner
account plus the `admin` scope on API tokens:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/storages` | List destinations with usage |
| `POST` | `/api/v1/storages` | Create a destination |
| `PATCH` | `/api/v1/storages/{id}` | Update a destination |
| `DELETE` | `/api/v1/storages/{id}` | Delete a destination (`confirm=true` when it holds backups) |
| `POST` | `/api/v1/storages/{id}/test` | Run the connection test |

The old `/api/v1/backups/providers` routes remain as a deprecated compatibility
alias for existing API tokens and MCP clients. They delegate to the same
service, accept the same payloads and sit behind the same owner + `admin` gate;
new clients should use `/storages`.
