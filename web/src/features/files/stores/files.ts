import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  chmodEntry,
  compressEntries,
  copyEntry,
  createFile,
  createFolder,
  deleteEntry,
  describeFilesError,
  extractEntry,
  filesErrorKey,
  isConflictError,
  isFilesDisabled,
  listEntries,
  listRoots,
  moveEntry,
  renameEntry,
} from "@/features/files/api/files";
import type { ArchiveFormat } from "@/features/files/api/files";
import type {
  FilesEntry,
  FilesRoot,
  FileKind,
} from "@/features/files/schemas/files";
import type { NameKind } from "@/features/files/utils/files";
import {
  breadcrumbSegments,
  countByKind,
  destinationProblem,
  entryMatchesQuery,
  isSelectable,
  joinPath,
  normalizeEntryName,
  parentPath,
  sortEntries,
} from "@/features/files/utils/files";
import type {
  BreadcrumbSegment,
  FilesSortDirection,
  FilesSortKey,
  FilesViewMode,
  TransferProblem,
} from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

/** FilesClipboard is a pending copy/cut of named entries from one directory. */
export interface FilesClipboard {
  mode: "copy" | "cut";
  root: string;
  path: string;
  names: string[];
  /** directories are the clipboard names that are folders, so pasting a folder
   *  into its own subtree is refused before the agent call (400). */
  directories: string[];
}

/** ConflictPrompt waits for the user to replace or skip 409'd names. */
export interface ConflictPrompt {
  op: "copy" | "move";
  source: string;
  destination: string;
  names: string[];
  /** cutClipboard clears the clipboard for a cut once the prompt resolves. */
  cutClipboard: boolean;
}

/** TransferSummary reports one batch move/copy/delete outcome. */
export interface TransferSummary {
  done: string[];
  conflicts: string[];
  failed: Array<{ name: string; error: string }>;
}

/**
 * EditorSync is the slice of the editor tab store the browser drives when a
 * mutation changes the filesystem under an open document. It is structural so
 * the store can be reached lazily (see {@link withEditor}) without a static
 * import.
 */
interface EditorSync {
  applyRename: (_root: string, _dir: string, _oldName: string, _newName: string) => void;
  applyMove: (_root: string, _fromDir: string, _toDir: string, _names: string[]) => void;
  applyDelete: (_root: string, _dir: string, _names: string[]) => void;
}

/**
 * withEditor reaches the editor tab store through a dynamic import. The files
 * store must not import it statically: the auth store (in the entry chunk)
 * resets the files store on sign-out, so a static import would drag the editor
 * store and its helpers into the entry bundle. The lazy import keeps them in
 * the files-page chunk while every mutation still routes its tab sync through
 * one place.
 */
async function withEditor(sync: (_editor: EditorSync) => void): Promise<void> {
  const { useEditorStore } = await import("@/features/files/stores/editor");
  sync(useEditorStore());
}

/**
 * Files browser store. Holds one server's browsable roots plus the current
 * directory page, the selection, the sort/filter, the view mode and the step-7
 * write surface (create, rename, delete, move/copy, clipboard, upload). Every
 * read and every write is team-scoped: the active team is read from the teams
 * store and sent as `X-Team-Id`.
 *
 * Concurrency: a single generation counter drops a response that lost to a
 * newer navigation or server switch, so a slow listing can never overwrite the
 * directory the user just opened. Reads are open to any member; content,
 * download and every mutation require the owner or admin role, enforced a
 * second time by the control plane.
 */
export const useFilesStore = defineStore("files", () => {
  const serverId = ref("");
  const roots = ref<FilesRoot[]>([]);
  const rootName = ref("");
  const path = ref("");
  const entries = ref<FilesEntry[]>([]);
  const cursor = ref("");
  const truncated = ref(false);
  const loading = ref(false);
  const loadingMore = ref(false);
  const loaded = ref(false);
  const disabled = ref(false);
  const errorRaw = ref<unknown>(null);
  const viewMode = ref<FilesViewMode>("list");
  const sortKey = ref<FilesSortKey>("name");
  const sortDir = ref<FilesSortDirection>("asc");
  const query = ref("");
  const selected = ref<Set<string>>(new Set());

  // ── Write state (P18 step 7) ──────────────────────────────────────────
  // The write surface mirrors the read gates: canWrite needs both a
  // write-capable role and a writable root. A read_only member or a
  // read-only root hides every mutation affordance and every mutating call
  // is refused by the control plane regardless.
  const acting = ref(false);
  const permissionDenied = ref(false);
  const clipboard = ref<FilesClipboard | null>(null);
  /** conflictPrompt is set when a transfer met 409s and awaits replace/skip. */
  const conflictPrompt = ref<ConflictPrompt | null>(null);

  // Bumped by every navigation/server switch. A response whose generation is
  // stale is discarded (see the plan's navigation race guard).
  let generation = 0;
  // Path of the last successfully loaded listing, so a failed refresh keeps
  // the visible entries while a failed navigation clears them.
  let lastLoadedPath = "";

  /** error renders the retained read failure in the active locale. */
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeFilesError(errorRaw.value),
  );

  /** currentRoot is the selected root object, if any. */
  const currentRoot = computed<FilesRoot | null>(
    () => roots.value.find((root) => root.name === rootName.value) ?? null,
  );

  /** writable marks the selected root writable (compose roots are not). */
  const writable = computed<boolean>(() => currentRoot.value?.writable ?? false);

  /** freeBytes is the selected root's free space (0 when unknown). */
  const freeBytes = computed<number>(() => currentRoot.value?.free_bytes ?? 0);

  /** breadcrumb is root → … → current path. */
  const breadcrumb = computed<BreadcrumbSegment[]>(() =>
    breadcrumbSegments(rootName.value, path.value),
  );

  /** hasMore reports a server cursor for the next page. */
  const hasMore = computed<boolean>(() => cursor.value !== "");

  const counts = computed(() => countByKind(entries.value));

  /** visibleEntries applies the folder filter, then the active sort. */
  const visibleEntries = computed<FilesEntry[]>(() => {
    const filtered =
      query.value.trim() === ""
        ? entries.value
        : entries.value.filter((entry) => entryMatchesQuery(entry, query.value));
    return sortEntries(filtered, sortKey.value, sortDir.value);
  });

  /** selectedNames is the current selection (insertion order). */
  const selectedNames = computed<string[]>(() => [...selected.value]);

  /** selectedEntries are the selected rows still present in the listing. */
  const selectedEntries = computed<FilesEntry[]>(() =>
    entries.value.filter((entry) => selected.value.has(entry.name)),
  );

  /** selectedBytes totals the selected entries' sizes. */
  const selectedBytes = computed<number>(() =>
    selectedEntries.value.reduce((sum, entry) => sum + entry.size, 0),
  );

  const selectableVisible = computed<FilesEntry[]>(() =>
    visibleEntries.value.filter(isSelectable),
  );

  /** allVisibleSelected drives the header select-all checkbox. */
  const allVisibleSelected = computed<boolean>(
    () =>
      selectableVisible.value.length > 0 &&
      selectableVisible.value.every((entry) => selected.value.has(entry.name)),
  );

  /** someVisibleSelected drives the header checkbox's indeterminate state. */
  const someVisibleSelected = computed<boolean>(
    () =>
      !allVisibleSelected.value &&
      selectableVisible.value.some((entry) => selected.value.has(entry.name)),
  );

  /**
   * canReadContent gates the owner/admin-only affordances (content and the
   * download ticket). An unknown role (teams not loaded) stays hidden rather
   * than promising a read the control plane would refuse.
   */
  const canReadContent = computed<boolean>(() => {
    const role = useTeamsStore().activeTeam?.role;
    return role === "owner" || role === "admin";
  });

  /**
   * canWrite gates every mutation: the caller must be able to write (owner or
   * admin) and the selected root must be writable (a compose root is not). It
   * is the single source of truth for hiding create/rename/delete/move/upload
   * affordances; the control plane refuses the call regardless.
   */
  const canWrite = computed<boolean>(() => canReadContent.value && writable.value);

  /** hasClipboard reports a non-empty clipboard for the paste action. */
  const hasClipboard = computed<boolean>(
    () => (clipboard.value?.names.length ?? 0) > 0,
  );

  /** clipboardMode reads the pending clipboard operation ("copy"/"cut"). */
  const clipboardMode = computed<"copy" | "cut" | null>(
    () => clipboard.value?.mode ?? null,
  );

  /**
   * pasteProblem explains why the clipboard cannot be pasted into the current
   * directory: another root, the clipboard's own directory, or a folder inside
   * a moved folder (all of which the agent refuses with 400). null means the
   * paste is valid. It mirrors TransferDialog's pre-check so paste() never
   * fires a doomed call.
   */
  const pasteProblem = computed<"cross-root" | TransferProblem | null>(() => {
    const clip = clipboard.value;
    if (!clip || clip.names.length === 0) {
      return null;
    }
    if (clip.root !== rootName.value) {
      return "cross-root";
    }
    return destinationProblem(
      clip.path,
      path.value,
      clip.directories.map((name) => ({ name, kind: "directory" as FileKind })),
    );
  });

  /** teamId reads the active team the requests are scoped to. */
  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** applyError records a read failure; a 503 flips the disabled state and a
   * 403 the distinct permission-denied state. */
  function applyError(err: unknown): void {
    if (isFilesDisabled(err)) {
      disabled.value = true;
      errorRaw.value = null;
      permissionDenied.value = false;
      return;
    }
    if (filesErrorKey(err) === "files.errors.forbidden") {
      permissionDenied.value = true;
      errorRaw.value = null;
      return;
    }
    errorRaw.value = err;
  }

  /** applyListing stores one page and marks the listing loaded. */
  function applyListing(entriesPage: FilesEntry[], nextCursor: string, isTruncated: boolean): void {
    entries.value = entriesPage;
    cursor.value = nextCursor;
    truncated.value = isTruncated;
    loaded.value = true;
    disabled.value = false;
    permissionDenied.value = false;
    lastLoadedPath = path.value;
    // Drop selection names that no longer exist (a refresh after another
    // session deleted them), so the bulk bar never counts a phantom row.
    pruneSelectionToEntries();
  }

  /** pruneSelectionToEntries keeps only selected names still in the listing. */
  function pruneSelectionToEntries(): void {
    if (selected.value.size === 0) {
      return;
    }
    const present = new Set(entries.value.map((entry) => entry.name));
    const next = new Set<string>();
    for (const name of selected.value) {
      if (present.has(name)) {
        next.add(name);
      }
    }
    if (next.size !== selected.value.size) {
      selected.value = next;
    }
  }

  /** renameSelection follows a renamed selected entry to its new name. */
  function renameSelection(oldName: string, nextName: string): void {
    if (!selected.value.has(oldName)) {
      return;
    }
    const next = new Set(selected.value);
    next.delete(oldName);
    next.add(nextName);
    selected.value = next;
  }

  /**
   * pruneClipboard drops names that no longer exist from the clipboard. It only
   * touches a clipboard taken from the same directory (root+path), so a delete
   * or rename in another directory never clears it or retargets it. The scope is
   * explicit for the cut-paste flow, where the moved names left the source
   * directory rather than the directory on screen.
   */
  function pruneClipboard(
    names: string[],
    root = rootName.value,
    dir = path.value,
  ): void {
    const clip = clipboard.value;
    if (!clip || names.length === 0) {
      return;
    }
    if (clip.root !== root || clip.path !== dir) {
      return;
    }
    const gone = new Set(names);
    const kept = clip.names.filter((name) => !gone.has(name));
    if (kept.length === clip.names.length) {
      return;
    }
    clipboard.value =
      kept.length === 0
        ? null
        : {
            ...clip,
            names: kept,
            directories: clip.directories.filter((name) => !gone.has(name)),
          };
  }

  /**
   * loadAll loads the roots, then the current directory. Used on first open
   * and by retry; navigation uses {@link loadEntries} alone.
   */
  async function loadAll(): Promise<void> {
    if (!serverId.value) {
      return;
    }
    const gen = ++generation;
    loading.value = true;
    errorRaw.value = null;
    disabled.value = false;
    try {
      const list = await listRoots(serverId.value, teamId());
      if (gen !== generation) {
        return;
      }
      roots.value = list;
      if (!list.some((root) => root.name === rootName.value)) {
        rootName.value = list[0]?.name ?? "";
      }
      if (rootName.value === "") {
        applyListing([], "", false);
        return;
      }
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: path.value,
      });
      if (gen !== generation) {
        return;
      }
      applyListing(
        listing.entries,
        listing.cursor ?? "",
        listing.truncated,
      );
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      applyError(err);
    } finally {
      if (gen === generation) {
        loading.value = false;
      }
    }
  }

  /** loadEntries loads the current directory only (navigation/refresh). */
  async function loadEntries(): Promise<void> {
    if (!serverId.value || rootName.value === "") {
      entries.value = [];
      cursor.value = "";
      truncated.value = false;
      return;
    }
    const gen = ++generation;
    loading.value = true;
    errorRaw.value = null;
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: path.value,
      });
      if (gen !== generation) {
        return;
      }
      applyListing(listing.entries, listing.cursor ?? "", listing.truncated);
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      applyError(err);
      // A failed navigation has no data for the new path; a failed refresh
      // keeps the entries already on screen.
      if (path.value !== lastLoadedPath) {
        entries.value = [];
        cursor.value = "";
        truncated.value = false;
      }
    } finally {
      if (gen === generation) {
        loading.value = false;
      }
    }
  }

  /**
   * init binds the store to one server. Switching servers resets the browser;
   * a repeat call for the same server is a no-op once it has loaded.
   */
  async function init(nextServerId: string): Promise<void> {
    if (serverId.value !== nextServerId) {
      resetBrowser();
      serverId.value = nextServerId;
    }
    if (loading.value) {
      return;
    }
    await loadAll();
  }

  /** refresh reloads the current directory. */
  async function refresh(): Promise<void> {
    await loadEntries();
  }

  /** retry re-runs the first load after a failure. */
  async function retry(): Promise<void> {
    if (roots.value.length === 0) {
      await loadAll();
      return;
    }
    await loadEntries();
  }

  /** loadMore appends the next cursor page. */
  async function loadMore(): Promise<void> {
    if (
      !serverId.value ||
      rootName.value === "" ||
      cursor.value === "" ||
      loadingMore.value
    ) {
      return;
    }
    const gen = generation;
    loadingMore.value = true;
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: path.value,
        cursor: cursor.value,
      });
      if (gen !== generation) {
        return;
      }
      entries.value = [...entries.value, ...listing.entries];
      cursor.value = listing.cursor ?? "";
      truncated.value = listing.truncated;
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      applyError(err);
    } finally {
      loadingMore.value = false;
    }
  }

  // ── Write actions (P18 step 7) ────────────────────────────────────────
  // Every mutation runs under `acting` (so the dialogs can disable their
  // confirm button and spin), refreshes the current directory on success, and
  // rethrows so the caller owns the localized error toast. A batch operation
  // reports a per-name outcome instead of aborting on the first failure.

  /** runMutation wraps one write in the acting flag. */
  async function runMutation<T>(task: () => Promise<T>): Promise<T> {
    acting.value = true;
    try {
      return await task();
    } finally {
      acting.value = false;
    }
  }

  /** createEntry creates an empty file or a folder in the current directory. */
  async function createEntry(name: string, kind: NameKind): Promise<FilesEntry> {
    const target = joinPath(path.value, normalizeEntryName(name));
    return runMutation(async () => {
      const created =
        kind === "directory"
          ? await createFolder(serverId.value, teamId(), {
              root: rootName.value,
              path: target,
            })
          : await createFile(serverId.value, teamId(), {
              root: rootName.value,
              path: target,
            });
      await loadEntries();
      return created;
    });
  }

  /** rename changes one entry's name inside the current directory. */
  async function rename(entry: FilesEntry, nextName: string): Promise<void> {
    const next = normalizeEntryName(nextName);
    await runMutation(async () => {
      await renameEntry(serverId.value, teamId(), {
        root: rootName.value,
        source: joinPath(path.value, entry.name),
        destination: joinPath(path.value, next),
      });
      // Follow the rename in the selection and any pending clipboard that was
      // taken from this same directory (root+path): a rename elsewhere must not
      // retarget the clipboard to a file in another folder.
      renameSelection(entry.name, next);
      await withEditor((editor) =>
        editor.applyRename(rootName.value, path.value, entry.name, next),
      );
      const clip = clipboard.value;
      if (
        clip &&
        clip.root === rootName.value &&
        clip.path === path.value &&
        clip.names.includes(entry.name)
      ) {
        clipboard.value = {
          ...clip,
          names: clip.names.map((name) => (name === entry.name ? next : name)),
          directories: clip.directories.map((name) =>
            name === entry.name ? next : name,
          ),
        };
      }
      await loadEntries();
    });
  }

  /** isDirectory reports whether one entry needs a recursive delete. */
  function isDirectory(entry: FilesEntry): boolean {
    return entry.kind === "directory";
  }

  /**
   * chmod changes one entry's permission bits and refreshes the listing. Only
   * the low 0o777 bits are sent; the agent refuses setuid/setgid/sticky. A
   * successful call re-loads the directory so the new mode shows immediately.
   */
  async function chmod(entry: FilesEntry, mode: number): Promise<void> {
    await runMutation(async () => {
      await chmodEntry(serverId.value, teamId(), {
        root: rootName.value,
        path: joinPath(path.value, entry.name),
        mode,
      });
      await loadEntries();
    });
  }

  /**
   * compress builds one archive from entries in the current directory and
   * returns the created archive's entry. It refreshes the listing on success
   * and rethrows so the dialog owns the localized error.
   */
  async function compress(
    entries: FilesEntry[],
    name: string,
    format: ArchiveFormat,
  ): Promise<FilesEntry> {
    const destination = joinPath(path.value, normalizeEntryName(name));
    return runMutation(async () => {
      const created = await compressEntries(serverId.value, teamId(), {
        root: rootName.value,
        sources: entries.map((entry) => joinPath(path.value, entry.name)),
        destination,
        format,
      });
      await loadEntries();
      return created;
    });
  }

  /**
   * extract unpacks one archive into the current directory. It refreshes the
   * listing on success and rethrows so the dialog owns the localized error.
   */
  async function extract(entry: FilesEntry, overwrite: boolean): Promise<void> {
    await runMutation(async () => {
      await extractEntry(serverId.value, teamId(), {
        root: rootName.value,
        source: joinPath(path.value, entry.name),
        destination: path.value,
        overwrite,
      });
      await loadEntries();
    });
  }

  /**
   * remove deletes entries, recursing into folders. It never aborts on one
   * failure: the summary reports the names that were deleted and the ones that
   * failed, so a partial batch still shows feedback for every entry.
   */
  async function remove(entries: FilesEntry[]): Promise<TransferSummary> {
    return runMutation(async () => {
      const summary: TransferSummary = { done: [], conflicts: [], failed: [] };
      for (const entry of entries) {
        try {
          await deleteEntry(
            serverId.value,
            teamId(),
            rootName.value,
            joinPath(path.value, entry.name),
            isDirectory(entry),
          );
          summary.done.push(entry.name);
        } catch (err) {
          summary.failed.push({ name: entry.name, error: describeFilesError(err) });
        }
      }
      pruneClipboard(summary.done);
      await withEditor((editor) =>
        editor.applyDelete(rootName.value, path.value, summary.done),
      );
      await loadEntries();
      return summary;
    });
  }

  /**
   * runTransfer copies or moves names from source into destination (same root),
   * one entry at a time so a single 409 or failure does not hide the rest. It
   * does not prompt: callers that want the replace/skip flow call
   * {@link promptTransfer}.
   */
  async function runTransfer(
    op: "copy" | "move",
    source: string,
    destination: string,
    names: string[],
    overwrite: boolean,
  ): Promise<TransferSummary> {
    const summary: TransferSummary = { done: [], conflicts: [], failed: [] };
    const call = op === "copy" ? copyEntry : moveEntry;
    for (const name of names) {
      try {
        await call(serverId.value, teamId(), {
          root: rootName.value,
          source: joinPath(source, name),
          destination: joinPath(destination, name),
          overwrite,
        });
        summary.done.push(name);
      } catch (err) {
        if (isConflictError(err)) {
          summary.conflicts.push(name);
        } else {
          summary.failed.push({ name, error: describeFilesError(err) });
        }
      }
    }
    if (op === "move" && summary.done.length > 0) {
      await withEditor((editor) =>
        editor.applyMove(rootName.value, source, destination, summary.done),
      );
    }
    await loadEntries();
    return summary;
  }

  /**
   * promptTransfer runs a transfer with overwrite=false and, when names clash,
   * opens the replace/skip prompt instead of silently failing them. cutClipboard
   * marks a clipboard cut so the moved names are dropped from it; failed or
   * skipped names stay for a later retry.
   */
  async function promptTransfer(
    op: "copy" | "move",
    source: string,
    destination: string,
    names: string[],
    cutClipboard = false,
  ): Promise<TransferSummary> {
    const summary = await runMutation(() =>
      runTransfer(op, source, destination, names, false),
    );
    conflictPrompt.value =
      summary.conflicts.length > 0
        ? { op, source, destination, names: [...summary.conflicts], cutClipboard }
        : null;
    // A cut clipboard is consumed only for the names that actually moved: a
    // failed or skipped entry stays so the user can retry it. The scope is the
    // source directory the names left, not the destination now on screen.
    if (cutClipboard && op === "move") {
      pruneClipboard(summary.done, rootName.value, source);
    }
    return summary;
  }

  /** resolveConflictPrompt retries the 409'd names, or drops them (skip). */
  async function resolveConflictPrompt(overwrite: boolean): Promise<TransferSummary> {
    const prompt = conflictPrompt.value;
    conflictPrompt.value = null;
    if (!prompt) {
      return { done: [], conflicts: [], failed: [] };
    }
    if (!overwrite) {
      // Skipped names stay in a cut clipboard so a later paste can retry them.
      return { done: [], conflicts: prompt.names, failed: [] };
    }
    const summary = await runMutation(() =>
      runTransfer(prompt.op, prompt.source, prompt.destination, prompt.names, true),
    );
    if (prompt.cutClipboard && prompt.op === "move") {
      pruneClipboard(summary.done, rootName.value, prompt.source);
    }
    return summary;
  }

  /**
   * copySelection copy-marks names (the current selection by default). A row
   * menu passes the single row it acted on, so it never has to mutate the
   * selection to stage a copy.
   */
  function copySelection(names: string[] = selectedNames.value): void {
    markClipboard("copy", names);
  }

  /** cutSelection cut-marks names (the current selection by default). */
  function cutSelection(names: string[] = selectedNames.value): void {
    markClipboard("cut", names);
  }

  /** markClipboard stores a non-empty clipboard for the current directory. */
  function markClipboard(mode: "copy" | "cut", names: string[]): void {
    if (names.length === 0) {
      return;
    }
    const wanted = new Set(names);
    const directories = entries.value
      .filter((entry) => entry.kind === "directory" && wanted.has(entry.name))
      .map((entry) => entry.name);
    clipboard.value = {
      mode,
      root: rootName.value,
      path: path.value,
      names: [...names],
      directories,
    };
  }

  /** clearClipboard drops any pending copy/cut. */
  function clearClipboard(): void {
    clipboard.value = null;
  }

  /**
   * paste applies the clipboard into the current directory. It refuses an
   * invalid destination up front (another root, the clipboard's own folder, or
   * a folder inside a moved folder), so the agent's 400 is never reached; the
   * caller reads {@link pasteProblem} to explain the refusal. A name clash opens
   * the replace/skip prompt; a cut clipboard is consumed only for the names that
   * actually moved.
   */
  async function paste(): Promise<TransferSummary> {
    const clip = clipboard.value;
    if (!clip || clip.names.length === 0 || pasteProblem.value !== null) {
      return { done: [], conflicts: [], failed: [] };
    }
    return promptTransfer(
      clip.mode === "copy" ? "copy" : "move",
      clip.path,
      path.value,
      clip.names,
      clip.mode === "cut",
    );
  }

  /** reload re-reads roots and the listing (a team switch changes both). */
  async function reload(): Promise<void> {
    roots.value = [];
    rootName.value = "";
    path.value = "";
    entries.value = [];
    cursor.value = "";
    truncated.value = false;
    loaded.value = false;
    errorRaw.value = null;
    permissionDenied.value = false;
    clearSelection();
    clearClipboard();
    conflictPrompt.value = null;
    await loadAll();
  }

  /** setRoot switches the browsable root and returns to its top level. */
  async function setRoot(name: string): Promise<void> {    if (name === rootName.value) {
      return;
    }
    rootName.value = name;
    path.value = "";
    clearSelection();
    await loadEntries();
  }

  /** openDirectory descends into one directory. */
  async function openDirectory(name: string): Promise<void> {
    path.value = joinPath(path.value, name);
    clearSelection();
    await loadEntries();
  }

  /** goToPath navigates to an absolute (root-relative) path. */
  async function goToPath(next: string): Promise<void> {
    if (next === path.value) {
      return;
    }
    path.value = next;
    clearSelection();
    await loadEntries();
  }

  /** goUp navigates to the parent directory (no-op at the root). */
  async function goUp(): Promise<void> {
    if (path.value === "") {
      return;
    }
    await goToPath(parentPath(path.value));
  }

  /** setSort cycles a column: same key flips the direction, a new key resets. */
  function setSort(key: FilesSortKey): void {
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
      return;
    }
    sortKey.value = key;
    sortDir.value = "asc";
  }

  /** setViewMode switches between the list and grid layouts. */
  function setViewMode(mode: FilesViewMode): void {
    viewMode.value = mode;
  }

  /** setQuery updates the client-side folder filter. */
  function setQuery(next: string): void {
    query.value = next;
  }

  /** isSelected reports whether one entry name is selected. */
  function isSelected(name: string): boolean {
    return selected.value.has(name);
  }

  /**
   * targetsForRow resolves a row-menu action's targets: the whole current
   * selection when the row is part of it, otherwise just that row. It never
   * toggles the selection, so cutting an already-selected row cannot deselect
   * it and cut an empty set.
   */
  function targetsForRow(entry: FilesEntry): FilesEntry[] {
    return selected.value.has(entry.name) ? selectedEntries.value : [entry];
  }

  /**
   * selectForRowContext applies the right-click selection rule: an unselected
   * row becomes the sole selection, while a row already inside the selection
   * keeps the whole multi-selection for the menu. A non-selectable entry is
   * left untouched so the menu still resolves it through {@link targetsForRow}.
   */
  function selectForRowContext(entry: FilesEntry): void {
    if (!isSelectable(entry) || selected.value.has(entry.name)) {
      return;
    }
    selected.value = new Set([entry.name]);
  }

  /** toggleSelection adds or removes one entry from the selection. */
  function toggleSelection(name: string): void {
    const next = new Set(selected.value);
    if (next.has(name)) {
      next.delete(name);
    } else {
      next.add(name);
    }
    selected.value = next;
  }

  /** toggleSelectAll adds every visible selectable entry, or clears them. */
  function toggleSelectAll(): void {
    const next = new Set(selected.value);
    if (allVisibleSelected.value) {
      for (const entry of selectableVisible.value) {
        next.delete(entry.name);
      }
    } else {
      for (const entry of selectableVisible.value) {
        next.add(entry.name);
      }
    }
    selected.value = next;
  }

  /** clearSelection drops the whole selection. */
  function clearSelection(): void {
    selected.value = new Set();
  }

  /** resetBrowser clears the listing but keeps the bound server id. */
  function resetBrowser(): void {
    generation += 1;
    roots.value = [];
    rootName.value = "";
    path.value = "";
    entries.value = [];
    cursor.value = "";
    truncated.value = false;
    loading.value = false;
    loadingMore.value = false;
    loaded.value = false;
    disabled.value = false;
    errorRaw.value = null;
    permissionDenied.value = false;
    acting.value = false;
    clipboard.value = null;
    conflictPrompt.value = null;
    viewMode.value = "list";
    sortKey.value = "name";
    sortDir.value = "asc";
    query.value = "";
    selected.value = new Set();
    lastLoadedPath = "";
  }

  /**
   * reset clears the browser and unbinds the server, so the next sign-in never
   * sees the previous account's listings. Called on sign-out (see the auth
   * store).
   */
  function reset(): void {
    resetBrowser();
    serverId.value = "";
  }

  return {
    // state
    serverId,
    roots,
    rootName,
    path,
    entries,
    cursor,
    truncated,
    loading,
    loadingMore,
    loaded,
    disabled,
    permissionDenied,
    acting,
    clipboard,
    clipboardMode,
    conflictPrompt,
    viewMode,
    sortKey,
    sortDir,
    query,
    // derived
    error,
    currentRoot,
    writable,
    freeBytes,
    breadcrumb,
    hasMore,
    counts,
    visibleEntries,
    selectedNames,
    selectedEntries,
    selectedBytes,
    allVisibleSelected,
    someVisibleSelected,
    canReadContent,
    canWrite,
    hasClipboard,
    pasteProblem,
    // actions
    init,
    refresh,
    retry,
    reload,
    loadMore,
    setRoot,
    openDirectory,
    goToPath,
    goUp,
    setSort,
    setViewMode,
    setQuery,
    isSelected,
    targetsForRow,
    selectForRowContext,
    toggleSelection,
    toggleSelectAll,
    clearSelection,
    createEntry,
    rename,
    chmod,
    remove,
    compress,
    extract,
    promptTransfer,
    resolveConflictPrompt,
    copySelection,
    cutSelection,
    clearClipboard,
    paste,
    reset,
  };
});
