<script setup lang="ts">
import { ChevronRight, MoreVertical } from "@lucide/vue";
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NDropdown,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSpin,
} from "naive-ui";
import type { DataTableColumns, DropdownOption } from "naive-ui";
import { resolveValidationMessage } from "@/shared/i18n";
import { computed, h, onMounted, ref, watch } from "vue";
import type { HTMLAttributes, VNode } from "vue";
import { useI18n } from "vue-i18n";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import FileTypeIcon from "@/features/files/components/FileTypeIcon.vue";
import { detectLanguage } from "@/features/files/utils/editor";
import type { EditorLanguageId } from "@/features/files/utils/editor";
import { entryIconKind } from "@/features/files/utils/files";
import type { FilesIconKind } from "@/features/files/utils/files";

import type {
  ContainerFileAction,
  ContainerFileCapabilities,
  ContainerFileEntry,
} from "@/features/applications/api/containerFiles";
import {
  changeContainerFiles,
  containerFileCapabilities,
  describeContainerFileError,
  destinationExists,
  downloadContainerFile,
  isContainerNotRunning,
  listContainerFiles,
  readContainerFile,
  uploadContainerFile,
  writeContainerFile,
} from "@/features/applications/api/containerFiles";
import type { ApplicationContainer } from "@/features/applications/api/containers";
import { listApplicationContainers } from "@/features/applications/api/containers";
import {
  containerFileDestinationSchema,
  containerFileModeSchema,
  containerFileNameSchema,
} from "@/features/applications/schemas/containerFiles";
import { containerFileMenuOptions } from "@/features/applications/utils/containerFileMenu";

interface Props {
  appId: string;
  canWrite: boolean;
}

type PromptKind = "mkdir" | "rename" | "copy" | "move" | "chmod";

interface PromptState {
  open: boolean;
  kind: PromptKind;
  entry: ContainerFileEntry | null;
  value: string;
  saving: boolean;
  error: string | null;
  confirmOverwrite: boolean;
}

const TEXT_LIMIT = 1024 * 1024;

// The tar fallback still pages a container without a shell; the ceiling keeps
// a broken cursor from looping forever.
const LISTING_PAGE_CEILING = 50;

const props = defineProps<Props>();
const { t } = useI18n();

const containers = ref<ApplicationContainer[]>([]);
const containersLoading = ref(false);
const containersError = ref(false);

const containerId = ref<string>("");
const capabilities = ref<ContainerFileCapabilities | null>(null);
const path = ref<string>("/");
const entries = ref<ContainerFileEntry[]>([]);
const truncated = ref(false);
const notRunning = ref(false);
const loading = ref(false);
const errorMessage = ref<string | null>(null);
const busy = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
let loadRun = 0;

const editor = ref<{
  open: boolean;
  entry: ContainerFileEntry | null;
  content: string;
  saving: boolean;
  error: string | null;
}>({ open: false, entry: null, content: "", saving: false, error: null });

const prompt = ref<PromptState>({
  open: false,
  kind: "mkdir",
  entry: null,
  value: "",
  saving: false,
  error: null,
  confirmOverwrite: false,
});

const deleteTarget = ref<ContainerFileEntry | null>(null);
const deleting = ref(false);

async function loadContainers(): Promise<void> {
  containersLoading.value = true;
  try {
    containers.value = await listApplicationContainers(props.appId);
    containersError.value = false;
  } catch {
    containers.value = [];
    containersError.value = true;
  } finally {
    containersLoading.value = false;
  }
}

onMounted(loadContainers);
watch(() => props.appId, () => void loadContainers());

const containerOptions = computed(() =>
  containers.value.map((container) => ({
    label: container.service
      ? `${container.service} · ${container.name || container.id}`
      : container.name || container.id,
    value: container.id,
  })),
);
const showPicker = computed<boolean>(() => containers.value.length > 1);

const canExec = computed<boolean>(() => capabilities.value?.exec_available === true);

const containerRunning = computed<boolean>(() => capabilities.value?.state === "running");

// A stopped container has no files to browse: the tab asks to start it
// instead of listing, and Refresh re-checks the state.
const needsStart = computed<boolean>(
  () => notRunning.value || (capabilities.value !== null && !containerRunning.value),
);

const execBlockedReason = computed<string>(() => {
  const blocked = capabilities.value?.exec_blocked_by ?? "";
  if (blocked === "not_running") {
    return String(t("applications.files.execBlocked.notRunning"));
  }
  if (blocked === "no_shell") {
    return String(t("applications.files.execBlocked.noShell"));
  }
  return String(t("applications.files.execBlocked.unsupported"));
});

const segments = computed<Array<{ name: string; path: string }>>(() => {
  const parts = path.value.split("/").filter((part) => part !== "");
  const rows: Array<{ name: string; path: string }> = [];
  let current = "";
  for (const part of parts) {
    current = `${current}/${part}`;
    rows.push({ name: part, path: current });
  }
  return rows;
});

const hasText = computed<boolean>(() => {
  const entry = editor.value.entry;
  return entry !== null && entry.size <= TEXT_LIMIT;
});

watch(
  containerOptions,
  (options) => {
    if (options.length === 0) {
      containerId.value = "";
      return;
    }
    if (!options.some((option) => option.value === containerId.value)) {
      containerId.value = options[0].value;
    }
  },
  { immediate: true },
);

watch(containerId, async (value) => {
  loadRun += 1;
  loading.value = false;
  path.value = "/";
  entries.value = [];
  truncated.value = false;
  notRunning.value = false;
  capabilities.value = null;
  errorMessage.value = null;
  if (value === "") {
    return;
  }
  const current = await loadCapabilities();
  if (current?.state === "running") {
    await load("/");
  }
});

async function loadCapabilities(): Promise<ContainerFileCapabilities | null> {
  if (containerId.value === "") {
    return null;
  }
  try {
    const next = await containerFileCapabilities(props.appId, containerId.value);
    capabilities.value = next;
    if (next.state === "running") {
      notRunning.value = false;
    }
    return next;
  } catch (err) {
    capabilities.value = null;
    errorMessage.value = describeContainerFileError(err);
    return null;
  }
}

async function load(directory: string): Promise<void> {
  const id = containerId.value;
  if (id === "") {
    return;
  }
  // A newer load (another folder or another container) owns the view; this
  // one stops writing, and never sends its cursor to the newer container.
  const run = ++loadRun;
  loading.value = true;
  try {
    let listing = await listContainerFiles(props.appId, id, directory);
    if (run !== loadRun) {
      return;
    }
    const collected = [...listing.entries];
    // An exec listing returns the whole folder at once. Only the tar fallback
    // (a container without a shell) still pages, so this loop follows its
    // cursor to the end instead of asking the user to load more.
    let pages = 1;
    while (listing.truncated && listing.cursor && pages < LISTING_PAGE_CEILING) {
      listing = await listContainerFiles(props.appId, id, directory, listing.cursor);
      if (run !== loadRun) {
        return;
      }
      collected.push(...listing.entries);
      pages += 1;
    }
    path.value = listing.directory?.path || directory || "/";
    entries.value = collected;
    truncated.value = listing.truncated;
    notRunning.value = false;
    errorMessage.value = null;
  } catch (err) {
    if (run !== loadRun) {
      return;
    }
    if (isContainerNotRunning(err)) {
      notRunning.value = true;
      entries.value = [];
      errorMessage.value = null;
    } else {
      errorMessage.value = describeContainerFileError(err);
    }
  } finally {
    if (run === loadRun) {
      loading.value = false;
    }
  }
}

async function refresh(): Promise<void> {
  await loadContainers();
  const current = await loadCapabilities();
  if (current?.state === "running") {
    await load(path.value);
  }
}

function openDirectory(target: string): void {
  void load(target);
}

function openEntry(entry: ContainerFileEntry): void {
  if (entry.kind === "directory") {
    openDirectory(entry.path);
    return;
  }
  if (entry.kind === "file" || entry.kind === "symlink") {
    void openEditor(entry);
  }
}

async function openEditor(entry: ContainerFileEntry): Promise<void> {
  if (entry.size > TEXT_LIMIT) {
    errorMessage.value = String(t("applications.files.editor.tooLarge"));
    return;
  }
  busy.value = true;
  try {
    const content = await readContainerFile(props.appId, containerId.value, entry.path);
    if (!content.isText) {
      errorMessage.value = String(t("applications.files.editor.notText"));
      return;
    }
    editor.value = { open: true, entry, content: content.text, saving: false, error: null };
  } catch (err) {
    errorMessage.value = describeContainerFileError(err);
  } finally {
    busy.value = false;
  }
}

async function saveEditor(): Promise<void> {
  const entry = editor.value.entry;
  if (entry === null) {
    return;
  }
  editor.value.saving = true;
  editor.value.error = null;
  try {
    await writeContainerFile(props.appId, containerId.value, entry.path, editor.value.content);
    editor.value.open = false;
    await load(path.value);
  } catch (err) {
    editor.value.error = describeContainerFileError(err);
  } finally {
    editor.value.saving = false;
  }
}

function pickFiles(): void {
  fileInput.value?.click();
}

async function onFilesPicked(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const files = Array.from(input.files ?? []);
  input.value = "";
  if (files.length === 0 || containerId.value === "") {
    return;
  }
  busy.value = true;
  try {
    for (const file of files) {
      await uploadContainerFile(props.appId, containerId.value, path.value, file);
    }
    await load(path.value);
  } catch (err) {
    errorMessage.value = describeContainerFileError(err);
  } finally {
    busy.value = false;
  }
}

async function download(entry: ContainerFileEntry): Promise<void> {
  if (entry.kind === "special") {
    return;
  }
  busy.value = true;
  try {
    const name = entry.kind === "directory" ? `${entry.name}.tar` : entry.name;
    await downloadContainerFile(props.appId, containerId.value, entry.path, name);
  } catch (err) {
    errorMessage.value = describeContainerFileError(err);
  } finally {
    busy.value = false;
  }
}

function parentOf(entry: ContainerFileEntry): string {
  const index = entry.path.lastIndexOf("/");
  return index <= 0 ? "/" : entry.path.slice(0, index);
}

function joinPath(directory: string, name: string): string {
  return directory === "/" ? `/${name}` : `${directory}/${name}`;
}

function openPrompt(kind: PromptKind, entry: ContainerFileEntry | null): void {
  let value = "";
  if (kind === "mkdir") {
    value = "";
  } else if (entry !== null) {
    value = kind === "rename" ? entry.name : joinPath(parentOf(entry), entry.name);
    if (kind === "chmod") {
      value = entry.mode.toString(8).padStart(3, "0");
    }
  }
  prompt.value = { open: true, kind, entry, value, saving: false, error: null, confirmOverwrite: false };
}

async function submitPrompt(): Promise<void> {
  const state = prompt.value;
  const entry = state.entry;
  state.error = null;
  const schema =
    state.kind === "chmod"
      ? containerFileModeSchema
      : state.kind === "copy" || state.kind === "move"
        ? containerFileDestinationSchema
        : containerFileNameSchema;
  const parsed = schema.safeParse(state.value);
  if (!parsed.success) {
    state.error = resolveValidationMessage(parsed.error.issues[0]?.message ?? "");
    return;
  }
  const value = parsed.data;
  state.saving = true;
  try {
    if (state.kind === "mkdir") {
      await changeContainerFiles(props.appId, containerId.value, "mkdir", {
        path: joinPath(path.value, value),
      });
    } else if (entry !== null) {
      if (state.kind === "rename") {
        await changeContainerFiles(props.appId, containerId.value, "rename", {
          path: entry.path,
          destination: joinPath(parentOf(entry), value),
        });
      } else if (state.kind === "chmod") {
        await changeContainerFiles(props.appId, containerId.value, "chmod", {
          path: entry.path,
          mode: parseInt(value, 8),
        });
      } else {
        const action = state.kind as "copy" | "move";
        try {
          await changeContainerFiles(props.appId, containerId.value, action, {
            path: entry.path,
            destination: value,
            overwrite: state.confirmOverwrite,
          });
        } catch (err) {
          if (!state.confirmOverwrite && destinationExists(err)) {
            state.confirmOverwrite = true;
            state.error = String(t("applications.files.prompt.overwriteQuestion"));
            return;
          }
          throw err;
        }
      }
    }
    prompt.value.open = false;
    await load(path.value);
  } catch (err) {
    state.error = describeContainerFileError(err);
  } finally {
    state.saving = false;
  }
}

async function confirmDelete(): Promise<void> {
  const entry = deleteTarget.value;
  if (entry === null) {
    return;
  }
  deleting.value = true;
  try {
    await changeContainerFiles(props.appId, containerId.value, "delete", { path: entry.path });
    deleteTarget.value = null;
    await load(path.value);
  } catch (err) {
    errorMessage.value = describeContainerFileError(err);
  } finally {
    deleting.value = false;
  }
}

function changeAction(action: ContainerFileAction, entry: ContainerFileEntry): void {
  if (!canExec.value) {
    return;
  }
  if (action === "delete") {
    deleteTarget.value = entry;
    return;
  }
  if (action === "copy" || action === "move" || action === "rename" || action === "chmod") {
    openPrompt(action, entry);
  }
}

function formatMode(mode: number): string {
  return (mode & 0o7777).toString(8).padStart(3, "0");
}

function formatSize(entry: ContainerFileEntry): string {
  if (entry.kind !== "file" && entry.kind !== "symlink") {
    return "—";
  }
  const units = ["B", "KiB", "MiB", "GiB"];
  let value = entry.size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value = value / 1024;
    unit += 1;
  }
  return unit === 0 ? `${value} B` : `${value.toFixed(1)} ${units[unit]}`;
}

function formatTime(entry: ContainerFileEntry): string {
  if (!entry.modified_at) {
    return "—";
  }
  const parsed = new Date(entry.modified_at);
  return Number.isNaN(parsed.getTime()) ? "—" : parsed.toLocaleString();
}

const selectedPath = ref("");

const menuEntry = ref<ContainerFileEntry | null>(null);
const menuOpen = ref(false);
const menuX = ref(0);
const menuY = ref(0);

const editorLanguage = computed<EditorLanguageId>(() =>
  editor.value.entry === null ? "plain" : detectLanguage(editor.value.entry.path),
);

const menuOptions = computed<DropdownOption[]>(() => {
  const entry = menuEntry.value;
  if (entry === null) {
    return [];
  }
  return containerFileMenuOptions(entry, {
    canWrite: props.canWrite,
    canExec: canExec.value,
    execReason: execBlockedReason.value,
    t: (key: string) => String(t(key)),
  });
});

function openMenu(entry: ContainerFileEntry, x: number, y: number): void {
  menuEntry.value = entry;
  menuX.value = x;
  menuY.value = y;
  menuOpen.value = true;
}

function onMenuSelect(key: string): void {
  menuOpen.value = false;
  const entry = menuEntry.value;
  if (entry === null) {
    return;
  }
  if (key === "open") {
    openDirectory(entry.path);
  } else if (key === "edit") {
    void openEditor(entry);
  } else if (key === "download") {
    void download(entry);
  } else {
    changeAction(key as ContainerFileAction, entry);
  }
}

function iconKind(entry: ContainerFileEntry): FilesIconKind {
  // entryIconKind reads the kind and the name; the rest fills the server entry
  // shape the shared helper expects.
  return entryIconKind({ ...entry, modified_at: "", etag: "" });
}

// The row's own handlers must not swallow events aimed at the row controls:
// Enter on the "..." button and a double click on it belong to the menu.
const ROW_CONTROL_SELECTOR = ".app-files-more";

function isRowControl(event: Event): boolean {
  return (
    event.target instanceof Element &&
    event.target.closest(ROW_CONTROL_SELECTOR) !== null
  );
}

function rowProps(entry: ContainerFileEntry): HTMLAttributes {
  return {
    class: [
      "app-files-row",
      entry.path === selectedPath.value ? "is-selected" : "",
    ],
    tabindex: 0,
    onClick: () => {
      selectedPath.value = entry.path;
    },
    onDblclick: (event: MouseEvent) => {
      if (isRowControl(event)) {
        return;
      }
      openEntry(entry);
    },
    onKeydown: (event: KeyboardEvent) => {
      if (event.key !== "Enter" || isRowControl(event)) {
        return;
      }
      event.preventDefault();
      openEntry(entry);
    },
    onContextmenu: (event: MouseEvent) => {
      event.preventDefault();
      openMenu(entry, event.clientX, event.clientY);
    },
  };
}

function nameCell(entry: ContainerFileEntry): VNode {
  return h("div", { class: "app-files-cell-name", "data-entry": entry.path }, [
    h(FileTypeIcon, { kind: iconKind(entry) }),
    h("span", { class: "app-files-entry mono", title: entry.name }, entry.name),
    entry.kind === "symlink" && entry.symlink_target
      ? h(
          "span",
          { class: "app-files-target mono muted", title: entry.symlink_target },
          ` → ${entry.symlink_target}`,
        )
      : null,
  ]);
}

function moreCell(entry: ContainerFileEntry): VNode {
  return h(
    NButton,
    {
      size: "tiny",
      quaternary: true,
      class: "app-files-more",
      "aria-haspopup": "menu",
      "aria-label": String(t("applications.files.rowMenu", { name: entry.name })),
      onClick: (event: MouseEvent) => {
        const box = (event.currentTarget as HTMLElement).getBoundingClientRect();
        openMenu(entry, box.left, box.bottom);
      },
    },
    { icon: () => h(MoreVertical, { size: 16, "aria-hidden": "true" }) },
  );
}

const columns = computed<DataTableColumns<ContainerFileEntry>>(() => [
  {
    title: t("applications.files.table.name"),
    key: "name",
    minWidth: 220,
    render: nameCell,
  },
  {
    title: t("applications.files.table.size"),
    key: "size",
    width: 110,
    render: (entry) =>
      h("span", { class: "mono muted app-files-cell" }, formatSize(entry)),
  },
  {
    title: t("applications.files.table.mode"),
    key: "mode",
    width: 90,
    render: (entry) =>
      h("span", { class: "mono app-files-cell" }, formatMode(entry.mode)),
  },
  {
    title: t("applications.files.table.owner"),
    key: "owner",
    width: 100,
    render: (entry) =>
      h("span", { class: "mono muted app-files-cell" }, `${entry.uid}:${entry.gid}`),
  },
  {
    title: t("applications.files.table.modified"),
    key: "modified",
    width: 170,
    render: (entry) =>
      h("span", { class: "muted app-files-cell" }, formatTime(entry)),
  },
  {
    title: "",
    key: "actions",
    width: 52,
    render: moreCell,
  },
]);

function rowKey(entry: ContainerFileEntry): string {
  return entry.path;
}

const promptTitle = computed<string>(() => {
  switch (prompt.value.kind) {
    case "mkdir":
      return String(t("applications.files.prompt.mkdirTitle"));
    case "rename":
      return String(t("applications.files.prompt.renameTitle"));
    case "copy":
      return String(t("applications.files.prompt.copyTitle"));
    case "move":
      return String(t("applications.files.prompt.moveTitle"));
    default:
      return String(t("applications.files.prompt.chmodTitle"));
  }
});

const promptLabel = computed<string>(() => {
  switch (prompt.value.kind) {
    case "mkdir":
      return String(t("applications.files.prompt.folderName"));
    case "chmod":
      return String(t("applications.files.prompt.mode"));
    default:
      return String(t("applications.files.prompt.destination"));
  }
});

const promptPlaceholder = computed<string>(() => {
  switch (prompt.value.kind) {
    case "mkdir":
      return String(t("applications.files.prompt.folderPlaceholder"));
    case "rename":
      return String(t("applications.files.prompt.renamePlaceholder"));
    case "chmod":
      return "644";
    default:
      return "/path/inside/container";
  }
});
</script>

<template>
  <NCard class="app-files" :title="t('applications.files.title')">
    <template #header-extra>
      <NSpace align="center" :size="8">
        <NButton
          size="small"
          secondary
          :loading="loading || containersLoading"
          data-testid="files-refresh"
          @click="refresh"
        >
          {{ t("applications.files.refresh") }}
        </NButton>
      </NSpace>
    </template>

    <div class="app-files-note-row">
      <NAlert
        type="warning"
        :show-icon="true"
        class="app-files-note app-files-note-compact"
        data-testid="files-persistent-note"
      >
        {{ t("applications.files.persistentNote") }}
      </NAlert>

      <div v-if="showPicker" class="app-files-picker">
        <label class="app-files-picker-label" for="app-files-container">
          {{ t("applications.files.containerLabel") }}
        </label>
        <NSelect
          id="app-files-container"
          size="small"
          class="app-files-picker-select"
          :value="containerId"
          :options="containerOptions"
          :loading="containersLoading"
          :placeholder="t('applications.files.chooseContainer')"
          data-testid="files-container-picker"
          @update:value="containerId = $event"
        />
      </div>
    </div>

    <NAlert v-if="!canWrite" type="info" :show-icon="true" class="app-files-note">
      {{ t("applications.files.ownerOnly") }}
    </NAlert>

    <NAlert
      v-if="containersError"
      type="error"
      :show-icon="true"
      class="app-files-note"
      data-testid="files-containers-error"
    >
      {{ t("applications.files.containersError") }}
    </NAlert>

    <NEmpty
      v-if="containers.length === 0 && !containersLoading"
      :description="t('applications.files.noContainers')"
    />

    <NSpin v-else-if="containersLoading" size="small" />

    <template v-else>
      <NAlert
        v-if="capabilities && containerRunning && !capabilities.exec_available"
        type="info"
        :show-icon="true"
        class="app-files-note"
        data-testid="files-exec-blocked"
      >
        <template #header>{{ t("applications.files.execBlocked.title") }}</template>
        {{ execBlockedReason }}
      </NAlert>

      <NAlert
        v-if="errorMessage"
        type="error"
        :show-icon="true"
        class="app-files-note"
        data-testid="files-error"
      >
        {{ errorMessage }}
      </NAlert>

      <NEmpty
        v-if="needsStart"
        data-testid="files-start-container"
        :description="t('applications.files.startContainer')"
      />

      <template v-else>
        <div class="app-files-toolbar">
          <nav class="app-files-breadcrumb" aria-label="breadcrumb">
            <NButton
              text
              size="small"
              data-testid="files-breadcrumb-root"
              @click="openDirectory('/')"
            >
              /
            </NButton>
            <template v-for="segment in segments" :key="segment.path">
              <ChevronRight class="app-files-crumb-sep" :size="14" aria-hidden="true" />
              <NButton
                text
                size="small"
                :data-testid="`files-breadcrumb-${segment.name}`"
                @click="openDirectory(segment.path)"
              >
                {{ segment.name }}
              </NButton>
            </template>
          </nav>

          <NSpace v-if="canWrite" align="center" :size="8">
            <NButton
              size="small"
              secondary
              data-testid="files-upload"
              :disabled="busy"
              @click="pickFiles"
            >
              {{ t("applications.files.upload") }}
            </NButton>
            <NButton
              size="small"
              secondary
              data-testid="files-new-folder"
              :disabled="busy || !canExec"
              :title="!canExec ? execBlockedReason : t('applications.files.newFolder')"
              @click="openPrompt('mkdir', null)"
            >
              {{ t("applications.files.newFolder") }}
            </NButton>
            <input
              ref="fileInput"
              class="app-files-input"
              type="file"
              multiple
              data-testid="files-input"
              @change="onFilesPicked"
            />
          </NSpace>
        </div>

        <NDataTable
          class="app-files-table"
          :columns="columns"
          :data="entries"
          :loading="loading"
          :row-key="rowKey"
          :row-props="rowProps"
          :bordered="false"
          :max-height="420"
          :min-row-height="34"
          :virtual-scroll="true"
          data-testid="files-table"
        >
          <template #empty>
            <NEmpty :description="t('applications.files.empty')" />
          </template>
        </NDataTable>

        <div v-if="truncated" class="app-files-truncated">
          <span class="muted">{{ t("applications.files.truncated") }}</span>
        </div>
      </template>
    </template>
  </NCard>

  <NDropdown
    trigger="manual"
    placement="bottom-start"
    :show="menuOpen"
    :x="menuX"
    :y="menuY"
    :options="menuOptions"
    @select="onMenuSelect"
    @clickoutside="menuOpen = false"
  >
    <span class="app-files-menu-anchor" aria-hidden="true"></span>
  </NDropdown>

  <NModal
    :show="editor.open"
    preset="card"
    class="app-modal standard-width"
    :title="t('applications.files.editor.title', { name: editor.entry?.name ?? '' })"
    :mask-closable="!editor.saving"
    @update:show="editor.open = $event"
  >
    <NAlert v-if="editor.error" type="error" :show-icon="true" class="app-files-note">
      {{ editor.error }}
    </NAlert>
    <CodeEditor
      v-if="editor.open"
      v-model="editor.content"
      :language="editorLanguage"
      :read-only="editor.saving || !hasText"
      height="min(60vh, 480px)"
      data-testid="files-editor-input"
      @save="saveEditor"
    />
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton :disabled="editor.saving" @click="editor.open = false">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="editor.saving"
          :disabled="!hasText"
          data-testid="files-editor-save"
          @click="saveEditor"
        >
          {{ t("applications.files.editor.save") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    :show="prompt.open"
    preset="card"
    class="app-modal standard-width"
    :title="promptTitle"
    :mask-closable="!prompt.saving"
    @update:show="prompt.open = $event"
  >
    <NAlert v-if="prompt.error" type="error" :show-icon="true" class="app-files-note" data-testid="files-prompt-error">
      {{ prompt.error }}
    </NAlert>
    <label class="app-files-picker-label" for="app-files-prompt-value">{{ promptLabel }}</label>
    <NInput
      id="app-files-prompt-value"
      v-model:value="prompt.value"
      :placeholder="promptPlaceholder"
      data-testid="files-prompt-input"
      @update:value="prompt.confirmOverwrite = false"
      @keyup.enter="submitPrompt"
    />
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton :disabled="prompt.saving" @click="prompt.open = false">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="prompt.saving"
          data-testid="files-prompt-submit"
          @click="submitPrompt"
        >
          {{ prompt.confirmOverwrite ? t("applications.files.prompt.overwriteConfirm") : t("common.actions.save") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    :show="deleteTarget !== null"
    preset="card"
    class="app-modal standard-width"
    :title="t('applications.files.delete.title')"
    :mask-closable="!deleting"
    @update:show="deleteTarget = null"
  >
    <p data-testid="files-delete-body">
      {{ t("applications.files.delete.body", { name: deleteTarget?.name ?? "" }) }}
    </p>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton :disabled="deleting" @click="deleteTarget = null">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="error"
          :loading="deleting"
          data-testid="files-delete-confirm"
          @click="confirmDelete"
        >
          {{ t("applications.files.actions.delete") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.app-files-note {
  margin-bottom: var(--space-3);
}

.app-files-note-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
}

.app-files-note-row .app-files-note {
  margin-bottom: 0;
}

.app-files-note-compact {
  flex: 1 1 20rem;
  min-width: 0;
}

.app-files-note-compact :deep(.n-alert-body) {
  padding-top: var(--space-1);
  padding-bottom: var(--space-1);
}

.app-files-note-compact :deep(.n-alert-body__content) {
  font-size: var(--text-xs);
  line-height: var(--leading-body);
}

/* The compact body is shorter than the default alert, so the icon has to sit
   inside it instead of hanging below the top margin. */
.app-files-note-compact :deep(.n-alert__icon) {
  margin-top: 0;
  font-size: var(--text-base);
}

.app-files-picker {
  display: flex;
  flex: 1 1 12rem;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  max-width: 18rem;
}

.app-files-picker-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.app-files-picker-select {
  flex: 1 1 auto;
  min-width: 0;
}

.app-files-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  margin-bottom: var(--space-1);
}

.app-files-breadcrumb {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
}

.app-files-crumb-sep {
  color: var(--muted);
}

.app-files-table {
  --fm-mark-size: var(--space-6);
}

.app-files-table :deep(.n-data-table-th),
.app-files-table :deep(.n-data-table-td) {
  padding: var(--space-1) var(--space-2);
  font-size: var(--text-xs);
}

.app-files-table :deep(.app-files-row) {
  cursor: default;
}

.app-files-table :deep(.app-files-row.is-selected) {
  background: var(--hover-row);
}

.app-files-table :deep(.app-files-cell-name) {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.app-files-table :deep(.app-files-entry),
.app-files-table :deep(.app-files-target),
.app-files-table :deep(.app-files-cell) {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.app-files-table :deep(.app-files-target) {
  font-size: var(--text-xs);
}

.app-files-table :deep(.app-files-more) {
  opacity: 0;
  transition: opacity var(--motion-base) var(--ease-standard);
}

.app-files-table :deep(.app-files-row:hover .app-files-more),
.app-files-table :deep(.app-files-row:focus-within .app-files-more) {
  opacity: 1;
}

/* No hover on touch, so the row menu button must always be visible there and
   keep a finger-sized hit area in the smaller row. */
@media (hover: none) {
  .app-files-table :deep(.app-files-more) {
    opacity: 1;
    min-width: var(--space-8);
    min-height: var(--space-8);
  }
}

.app-files-menu-anchor {
  position: fixed;
  width: 0;
  height: 0;
}

.app-files-truncated {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-1);
}

.app-files-input {
  display: none;
}
</style>
