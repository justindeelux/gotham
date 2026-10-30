---
title: Applications
---

# Applications

An application is one deployable workload: a source, a build, a container and
its routing. Applications live inside an environment, so their address keeps the
whole path:
`/projects/{project}/environments/{environment}/applications/{id}`.

There is no top-level Applications list. Open the project, open the environment,
then use the **Applications** tab or **Add resource**.

## Create an application

Choose **Add resource → Application**, then pick a source. The wizard walks
through **Source**, **Build pack**, **Runtime**, **Env & storage** and
**Deploy**; the review step creates the application and queues the first deploy.

- The application name is 3–31 characters: a lowercase letter, then lowercase
  letters, digits or dashes. It is used for the container and image tag.
- **Runtime** selects the node. Every resource is pinned to one node.
- The source and build pack are fixed after creation. The node can be changed
  later in **Settings**, and the source content (Dockerfile text, compose
  document, image reference) can be edited from **Overview**.

The first deploy starts with no route. Add hostnames afterwards on the
**Domains** tab; see [Domains and previews](/using/domains-previews).

## Sources

| Source | What it uses |
|---|---|
| Public git repository | An `http(s)` or `git://` clone URL with no stored credentials. |
| Private git repository | A clone URL plus an SSH deploy key or an HTTPS token. |
| GitHub (connected) | A connected GitHub account; the wizard lists its repositories and branches. |
| GitLab (connected) | A connected GitLab account, with the same repository and branch picker. |
| Dockerfile | Pasted Dockerfile text. No repository is cloned. |
| Docker Compose | A pasted compose document, or a compose file path inside a repository. |
| Container image | A prebuilt reference from any registry, with optional credentials. |

**Private git.** The application gets its own ed25519 deploy key, or an HTTPS
token stored sealed. Open **Overview → Private git source**, copy the public key
to the repository as a read-only deploy key (or save the token), and use **Test
connection** before deploying. Both credentials can be replaced or removed
there.

**Connected providers.** Connect a GitHub App or a GitLab connection once under
**Settings → Git sources**, then pick the account and repository in the wizard.
Connections belong to your account, and a private repository still deploys with
an SSH deploy key.

**Dockerfile.** The pasted text is the entire build context; nothing is cloned,
and a redeploy builds the latest saved text. It must contain a `FROM`
instruction and is limited to 64 KiB. Build args are optional and stored in
plaintext, including in image history — keep credentials in environment
variables or sealed secrets instead.

**Docker Compose.** Paste the document (up to 256 KiB) or point at a file path
inside a repository. Choose the web service that domains and ports route to.
Deploys pull the images the document declares and run the project with
`up -d`; the document is stored and re-applied by a rollback. Use environment
variables for values, never secrets pasted into the document.

Compose also appears on the **Service** tab of the add-resource picker, where
the compose project is managed as a service instead of an application. See
[Services and templates](/using/services-templates).

**Container image.** Enter `registry/repo:tag`, optionally pinned with a digest
(`@sha256:…`). A private registry can store a username and token; the token is
sealed and never shown again. A redeploy pulls the tag again, while a rollback
uses the digest recorded for that release.

## Builds

Four build packs are supported; pick one in the **Build pack** step or leave it
on auto-detect:

- **Dockerfile** builds the Dockerfile in the repository.
- **Railpack** builds a language project without a Dockerfile; it needs the
  Railpack, BuildKit and Docker host tools.
- **Buildpacks** builds with Cloud Native Buildpacks; it needs the `pack` host
  tool.
- **Static** builds a static directory and serves it through nginx; no process
  runs.

With no pack chosen, detection tries Dockerfile, then Railpack, then
Buildpacks, then static. A missing required host tool is reported at deploy
time; install the tools from [App store](/operating/app-store).

A pasted Dockerfile source always builds from its saved text. Compose and
container-image sources have no build pack: compose runs the document's
services, and an image source pulls the reference.

## Deploy pipeline

Each deploy is recorded and walks these stages (some sources skip the first
two):

| Stage | What happens |
|---|---|
| `queued` | The deploy is accepted and waits for the worker. |
| `cloning` | The source is fetched, or the saved Dockerfile or compose document is resolved. |
| `building` | The image is built, or the compose document is rendered. |
| `pushing` | The built image is pushed to the node's registry, or the source images are pulled. |
| `starting` | The container or compose project starts and passes its health check. |
| `running` | The release is live. |
| `failed` | A stage failed; the error is recorded on the deployment. |

A container-image deploy skips cloning and building because there is no source
to clone and no image to build. A rollback of an image-based release also skips
both stages: it reuses the image already pushed (an image source pulls the
recorded digest). A compose rollback re-renders and re-applies the stored
document.

The **Overview** tab shows this pipeline for the latest deployment, with each
stage marked done, active or pending. A failed deployment does not know which
stage failed, so it renders every stage as pending and adds a terminal `failed`
node instead of claiming later stages ran.

## Deploy and operate

On an application with no deployments, **Deploy now** on **Overview** queues the
first one. **Redeploy** in the page header queues a new deployment of the current
source at any time. A redeploy pulls the branch again, builds the saved
Dockerfile again, pulls the compose images again, or pulls the image tag again,
depending on the source.

**Rollback** in the header, or on a deployment row, restores an earlier release.
The dialog lists the deployments that are still running and defaults to the
previous one. For image-based applications the control plane switches the image
tag and restarts the container; the environment and volumes stay unchanged, the
old image stays in the internal registry, and the container it replaces is kept
so you can roll forward again. A compose rollback re-applies the stored compose
file with the current environment and secrets.

**Stop** and **Start** control the container of the newest deployment (for a
compose application, the routed web container). A stop leaves the deployment
history intact — the release still reads as running and the header shows
`stopped` — and a start brings the same container back. These buttons are
disabled while a deploy is in flight or when there is no container yet; deploy
the application first. A stopped application has no running container for a
terminal session, so the **Terminal** tab asks you to start it.

## Deployment history

The **Deployments** tab lists every attempt for the application. Each row shows
the deploy id, kind (`deploy` or `rollback`), commit, image, duration, state,
error, creation time and actions.

**Logs** opens the build log for that deployment. **Rollback** is offered only
on a deployment that is still running; it opens the same rollback dialog with
that release preselected.

## Logs

The **Logs** tab has two panes:

- **Deployment log** shows the build output of the selected deployment. While
  the deploy runs it streams live; once the deployment finishes, the stored log
  is shown, and a deployment that produced no stored log says so.
- **Runtime log** streams the running container's output. It needs a running
  container, and it streams from the application's own node.

## Detail tabs

| Tab | What it shows |
|---|---|
| **Overview** | Source, branch, build pack, domain and node; latest pipeline and recent deployments; editors for the private-git credential, Dockerfile, compose document or image reference. |
| **Deployments** | Full deployment history and its log and rollback actions. |
| **Logs** | Build log for one deployment and the live runtime log. |
| **Environment** | The application's own variables, with inherited shared variables shown read-only. |
| **Terminal** | A shell inside a running service container. The owner only; choose the service container when the application runs more than one. |
| **Storage** | Volumes and their container mounts. Data lives on the node and survives redeploys and rollbacks. |
| **Domains** | Hostnames, target ports and certificate configuration. See [Domains and previews](/using/domains-previews). |
| **Backups** | Runs and schedules for the application's persistent data. |
| **Previews** | Pull-request preview deployments. See [Domains and previews](/using/domains-previews). |
| **Settings** | Move the application to another environment or node, and the danger zone for deletion. |

The Deployments and Previews tabs show their counts. Storage appears once the
application has loaded, and Previews when preview deployments are enabled on
the instance. The Terminal tab is owner-only.

## Backups

The **Backups** tab captures the application's persistent data: its storage
mappings, or the named volumes of a compose application. The application's code
and image are rebuilt from source, so they are never part of a backup, and the
environment and configuration are not exported either.

The tab lists what is included, so a run never looks like it captures more than
it does. An application with no storage mapping has nothing to capture; add one
under **Storage** first, or the API refuses the run with a clear validation
error.

A backup stops the application while the archive is taken, then starts it again
if it was running. A schedule can choose **Do not stop the resource**: there is
no downtime, but files written during the archive may be inconsistent. A backup
is refused while a deploy, rollback, restart or restore of the same application
is in flight, and a deploy request during a backup is refused the same way.

Restoring stops the application, replaces its stored data with the artifact and
starts it again. The confirmation states the downtime and that current data is
replaced. If the import fails, the application stays stopped and the restore
history shows the error, so nobody serves half-restored data.

## Environment variables

The **Environment** tab edits variables for this application. Shared variables
from the environment and project are listed read-only above the editor.

The most specific value wins: application overrides environment, and
environment overrides project. Adding the same key here overrides the inherited
value at deploy time.

- Use the `^[A-Z][A-Z0-9_]*$` convention. The API only rejects structurally
  impossible names (empty, over 128 bytes, whitespace, `=` or NUL); a
  non-standard name is accepted but may not be injected as expected.
- Give a value the `secret:` prefix to store it sealed. The API returns a
  `secret:<id>` reference, never the plaintext; saving a stored secret untouched
  keeps its sealed value.
- **Save** replaces the whole collection. New values apply from the next deploy.

## Move or delete

Open **Settings** to change where the application runs. **Server and
environment → Change scope** selects another project and environment plus a
node; **Save location** applies it, and the page moves to the new nested route.

A move is refused while a deployment is in flight, when the target environment
already holds an application with the same name, and while a base application
still has live previews.

**Danger zone → Delete application** removes the application and requires typing
its name to confirm. It removes the containers, the named data volumes and the
stored configuration, and stops its routes; bind directories stay on the node,
so remove them separately if the data is no longer needed. A volume still in use
is reported as a warning and the delete still completes. A delete is refused
while a deploy is in flight.

## Access

You see this application because the owner granted its project to you. Inside a
granted project you can create, deploy, roll back, start, stop, change settings
and delete. A project that is not granted is hidden.

The owner manages nodes and the host surfaces around the application. See
[Members and access](/using/members-access).

## Resource history

The **Metrics** tab charts CPU, memory, network and disk I/O for the
application's managed containers over 1h, 24h, 7d and 30d ranges. The node agent
samples running containers about every 30 seconds; the control plane never
invents a point for a bucket it did not receive.

- CPU is in core units: `1.0` means one full core, `0.5` half a core.
- Memory is the working set: container usage minus reclaimable page cache. With
  no configured memory limit the chart shows bytes and **unlimited**, never a
  percentage of an invented limit. With a limit, the limit is shown separately.
- Network RX/TX and block read/write are bytes per second, calculated from
  monotonic counter deltas over the actual elapsed time. A first sample, a
  counter reset, a container restart or a nonpositive interval is a gap, never
  a negative or spiking rate.
- A **partial** badge means at least one replica of the service was not sampled
  in that range. The chart leaves that coverage gap visible.
- Deployment markers separate container incarnations. A redeploy keeps the
  application's history and starts a new segment instead of mixing the replaced
  container into the current service.
- Raw samples are kept for 24 hours, 1-minute rollups for 7 days and 1-hour
  rollups for 30 days. Each tier expires independently.
- Open the **Service** selector to isolate one Compose service when an
  application has several.

The API is `GET /api/v1/applications/{id}/metrics?range=1h|24h|7d|30d&service=`.
If the node's agent is too old to push container samples, the tab reports that
history is unavailable instead of drawing an empty healthy chart. Project
members see history only for applications in a granted project; unrelated
containers, raw host paths and server details are never included.
