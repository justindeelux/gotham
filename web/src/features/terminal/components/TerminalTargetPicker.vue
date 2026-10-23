<script setup lang="ts">
import { Plus, RefreshCw, Search } from "@lucide/vue";
import { NButton, NInput, NPopover, NSpin } from "naive-ui";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { listTerminalTargets, terminalFailure } from "@/features/terminal/api/terminal";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type {
  TerminalHostTarget,
  TerminalTargetErrorCode,
  TerminalTargetGroup,
  TerminalTargets,
} from "@/features/terminal/schemas/terminal";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalOpenOptions } from "@/features/terminal/stores/terminal";
import { useTeamsStore } from "@/features/teams/stores/teams";
import { containerTabTitle } from "@/features/terminal/utils/containerRecovery";

interface Props {
  surface: "window" | "page";
}

interface TargetRow {
  key: string;
  kind: "server" | "container";
  label: string;
  meta: string;
  options: TerminalOpenOptions;
}

interface TargetGroupView {
  key: string;
  label: string;
  rows: TargetRow[];
}

const props = defineProps<Props>();
const emit = defineEmits<{ opened: [tabId: string] }>();
const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const terminal = useTerminalStore();
const teams = useTeamsStore();

const show = ref(false);
const loading = ref(false);
const loadError = ref<TerminalErrorCode | "">("");
const targets = ref<TerminalTargets | null>(null);
const query = ref("");
const panel = ref<HTMLElement | null>(null);
const searchInput = ref<InstanceType<typeof NInput> | null>(null);
const trigger = ref<InstanceType<typeof NButton> | null>(null);
const pendingKey = ref("");
const pickError = ref(false);
let picked = false;
let loadToken = 0;
let controller: AbortController | null = null;

function sourcePath(serverId: string): string {
  if (props.surface !== "page") {
    return "";
  }
  return router.resolve({
    name: "terminal",
    query: { ...route.query, server: serverId },
  }).fullPath;
}

function serverRow(server: TerminalHostTarget): TargetRow {
  return {
    key: `server:${server.id}`,
    kind: "server",
    label: server.name,
    meta: t("terminal.targets.hostShell"),
    options: {
      serverId: server.id,
      serverName: server.name,
      sourcePath: sourcePath(server.id),
    },
  };
}

function containerRows(group: TerminalTargetGroup): TargetRow[] {
  return group.containers.map((container) => {
    const label = container.label || container.name;
    return {
      key: `container:${group.application_id}:${container.service}:${container.container_id}`,
      kind: "container",
      label,
      meta:
        container.service === ""
          ? t("terminal.targets.replicaOnly", { replica: container.replica })
          : t("terminal.targets.containerMeta", {
              service: container.service,
              replica: container.replica,
            }),
      options: {
        serverId: group.server_id,
        serverName: "",
        target: "container",
        applicationId: group.application_id,
        applicationName: group.application_name,
        service: container.service,
        containerId: container.container_id,
        containerName: container.name,
        targetLabel: label,
        title: containerTabTitle(group.application_name, label),
        sourcePath: sourcePath(group.server_id),
      },
    };
  });
}

function matches(row: TargetRow, needle: string): boolean {
  if (needle === "") {
    return true;
  }
  return `${row.label} ${row.meta}`.toLowerCase().includes(needle);
}

const visibleTargetCount = computed<number>(() => {
  const value = targets.value;
  if (value === null) {
    return 0;
  }
  return value.applications.reduce(
    (total, group) => total + group.containers.length,
    value.servers.length,
  );
});

const groups = computed<TargetGroupView[]>(() => {
  const needle = query.value.trim().toLowerCase();
  const value = targets.value;
  if (value === null) {
    return [];
  }
  const result: TargetGroupView[] = [];
  const servers = value.servers.map(serverRow).filter((row) => matches(row, needle));
  if (servers.length > 0) {
    result.push({
      key: "servers",
      label: t("terminal.targets.servers"),
      rows: servers,
    });
  }
  for (const group of value.applications) {
    const rows = containerRows(group).filter((row) => matches(row, needle));
    if (rows.length === 0) {
      continue;
    }
    result.push({
      key: `application:${group.application_id}`,
      label: group.application_name,
      rows,
    });
  }
  return result;
});

function optionsInPanel(): HTMLButtonElement[] {
  const root = panel.value;
  if (root === null) {
    return [];
  }
  return [...root.querySelectorAll<HTMLButtonElement>(".term-target-option")];
}

function focusOption(index: number): void {
  const options = optionsInPanel();
  if (options.length === 0) {
    return;
  }
  const bounded = Math.min(Math.max(index, 0), options.length - 1);
  options[bounded]?.focus();
}

function onSearchKeydown(event: KeyboardEvent): void {
  if (event.key === "ArrowDown") {
    event.preventDefault();
    focusOption(0);
    return;
  }
  if (event.key === "Escape") {
    event.preventDefault();
    closePopover();
  }
}

function closePopover(): void {
  show.value = false;
  cancelLoad();
  focusTrigger();
}

function onOptionKeydown(event: KeyboardEvent): void {
  const options = optionsInPanel();
  const current = options.indexOf(event.currentTarget as HTMLButtonElement);
  if (current === -1) {
    return;
  }
  switch (event.key) {
    case "ArrowDown":
      event.preventDefault();
      focusOption(current + 1);
      break;
    case "ArrowUp":
      event.preventDefault();
      focusOption(current - 1);
      break;
    case "Home":
      event.preventDefault();
      focusOption(0);
      break;
    case "End":
      event.preventDefault();
      focusOption(options.length - 1);
      break;
    case "Escape":
      event.preventDefault();
      closePopover();
      break;
    default:
      break;
  }
}

function errorText(code: TerminalErrorCode): string {
  switch (code) {
    case "auth":
      return t("terminal.targets.errors.auth");
    case "network":
      return t("terminal.targets.errors.network");
    case "terminalDisabled":
      return t("terminal.targets.errors.disabled");
    case "badResponse":
      return t("terminal.targets.errors.internal");
    default:
      return t("terminal.targets.errors.unavailable");
  }
}

function sourceErrorText(code: TerminalTargetErrorCode): string {
  switch (code) {
    case "disabled":
      return t("terminal.targets.errors.disabled");
    case "internal":
      return t("terminal.targets.errors.internal");
    case "timeout":
      return t("terminal.targets.errors.timeout");
    default:
      return t("terminal.targets.errors.unavailable");
  }
}

function cancelLoad(): void {
  loadToken += 1;
  controller?.abort();
  controller = null;
  loading.value = false;
}

async function load(): Promise<void> {
  cancelLoad();
  const token = loadToken;
  const abort = new AbortController();
  controller = abort;
  loading.value = true;
  loadError.value = "";
  pickError.value = false;
  try {
    const result = await listTerminalTargets(teams.activeTeamId, abort.signal);
    if (token !== loadToken) {
      return;
    }
    targets.value = result;
  } catch (error) {
    if (token !== loadToken || abort.signal.aborted) {
      return;
    }
    targets.value = null;
    loadError.value = terminalFailure(error).code;
  } finally {
    if (token === loadToken) {
      loading.value = false;
      controller = null;
    }
  }
}

function onGlobalKeydown(event: KeyboardEvent): void {
  if (!show.value || event.key !== "Escape") {
    return;
  }
  event.preventDefault();
  closePopover();
}

function focusTrigger(): void {
  void nextTick(() => {
    window.setTimeout(() => {
      const element = trigger.value?.$el;
      if (element instanceof HTMLElement) {
        element.focus();
      }
    }, 0);
  });
}

function onShow(value: boolean): void {
  if (value) {
    query.value = "";
    pickError.value = false;
    picked = false;
    void load();
    void nextTick(() => searchInput.value?.focus());
    return;
  }
  cancelLoad();
  if (!picked) {
    focusTrigger();
  }
  picked = false;
}

watch(
  () => teams.activeTeamId,
  () => {
    if (show.value) {
      void load();
    }
  },
);

async function select(row: TargetRow): Promise<void> {
  if (pendingKey.value === row.key) {
    return;
  }
  pendingKey.value = row.key;
  pickError.value = false;
  try {
    const tab =
      props.surface === "window"
        ? await terminal.openWindowSession(row.options)
        : await terminal.openPageSession({
            ...row.options,
            fresh: true,
            sourcePath: row.options.sourcePath ?? "",
          });
    if (props.surface === "page") {
      terminal.activatePage(tab.id);
    }
    picked = true;
    show.value = false;
    emit("opened", tab.id);
    void nextTick(() => {
      const id =
        props.surface === "window"
          ? `term-tab-${tab.id}`
          : `term-page-tab-${tab.id}`;
      document.getElementById(id)?.focus();
    });
  } catch {
    pickError.value = true;
  } finally {
    pendingKey.value = "";
  }
}

onMounted(() => {
  window.addEventListener("keydown", onGlobalKeydown);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onGlobalKeydown);
  cancelLoad();
});
</script>

<template>
  <NPopover
    v-model:show="show"
    trigger="click"
    placement="bottom-start"
    :duration="0"
    :animated="false"
    @update:show="onShow"
  >
    <template #trigger>
      <NButton
        ref="trigger"
        quaternary
        size="tiny"
        class="term-add-tab"
        :aria-label="t('terminal.targets.open')"
        :title="t('terminal.targets.open')"
      >
        <template #icon>
          <Plus :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </template>
    <div
      ref="panel"
      class="term-targets"
      role="dialog"
      :aria-label="t('terminal.targets.title')"
    >
      <div class="term-targets-head">
        <span class="term-targets-title">{{ t("terminal.targets.title") }}</span>
      </div>
      <NInput
        ref="searchInput"
        v-model:value="query"
        size="small"
        clearable
        class="term-targets-search"
        :placeholder="t('terminal.targets.searchPlaceholder')"
        :aria-label="t('terminal.targets.search')"
        @keydown="onSearchKeydown"
      >
        <template #prefix>
          <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NInput>

      <div v-if="loading" class="term-targets-state" role="status" aria-live="polite">
        <NSpin size="small" :aria-label="t('terminal.targets.loading')" />
        <p class="small muted">{{ t("terminal.targets.loading") }}</p>
      </div>

      <div v-else-if="loadError !== ''" class="term-targets-state" role="alert">
        <p class="term-targets-error">{{ errorText(loadError) }}</p>
        <NButton size="tiny" secondary @click="load">
          <template #icon>
            <RefreshCw :size="13" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.targets.retry") }}
        </NButton>
      </div>

      <div v-else-if="groups.length === 0" class="term-targets-state">
        <template v-if="targets !== null && targets.errors.length > 0">
          <p class="term-targets-error" role="alert">
            {{ t("terminal.targets.partial") }}
          </p>
          <p
            v-for="(entry, index) in targets.errors"
            :key="`${entry.source}:${entry.id ?? ''}:${entry.code}:${index}`"
            class="term-targets-empty"
          >
            {{ sourceErrorText(entry.code) }}
          </p>
          <NButton size="tiny" secondary @click="load">
            <template #icon>
              <RefreshCw :size="13" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ t("terminal.targets.retry") }}
          </NButton>
        </template>
        <template v-else>
          <p class="term-targets-empty">
            {{
              query.trim() === ""
                ? t("terminal.targets.empty")
                : t("terminal.targets.noMatches")
            }}
          </p>
          <p v-if="query.trim() === ''" class="small muted">
            {{ t("terminal.targets.emptyHint") }}
          </p>
        </template>
      </div>

      <div v-else class="term-targets-scroll">
        <section
          v-for="group in groups"
          :key="group.key"
          class="term-targets-group"
        >
          <p class="term-targets-group-title">{{ group.label }}</p>
          <ul class="term-targets-list">
            <li v-for="row in group.rows" :key="row.key">
              <button
                type="button"
                class="term-target-option"
                :class="{
                  'is-pending': pendingKey === row.key,
                  'is-container': row.kind === 'container',
                }"
                :disabled="pendingKey === row.key"
                @click="select(row)"
                @keydown="onOptionKeydown"
              >
                <span class="term-target-label">{{ row.label }}</span>
                <span class="term-target-meta mono">
                  <template v-if="pendingKey === row.key">
                    {{ t("terminal.targets.opening") }}
                  </template>
                  <template v-else>{{ row.meta }}</template>
                </span>
              </button>
            </li>
          </ul>
        </section>
        <p v-if="targets !== null && targets.errors.length > 0" class="term-targets-partial">
          {{ t("terminal.targets.partial") }}
          <span
            v-for="(entry, index) in targets.errors"
            :key="`${entry.source}:${entry.id ?? ''}:${entry.code}:${index}`"
            class="term-targets-partial-item"
          >
            {{ sourceErrorText(entry.code) }}
            <template v-if="entry.count !== undefined">
              ({{ entry.count }})
            </template>
          </span>
        </p>
        <p v-if="targets?.truncated" class="term-targets-truncated">
          {{ t("terminal.targets.truncated", { count: visibleTargetCount }) }}
        </p>
      </div>

      <p v-if="pickError" class="term-targets-error" role="alert">
        {{ t("terminal.targets.createFailed") }}
      </p>
    </div>
  </NPopover>
</template>

<style scoped>
.term-targets {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: min(24rem, 80vw);
  padding: var(--space-1);
}

.term-targets-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.term-targets-title {
  font-weight: 600;
  color: var(--fg-2);
}

.term-targets-search {
  width: 100%;
}

.term-targets-state {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-1);
}

.term-targets-empty,
.term-targets-error {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-sm);
}

.term-targets-error {
  color: var(--danger);
}

.term-targets-scroll {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-height: min(20rem, 55vh);
  overflow-y: auto;
}

.term-targets-group {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.term-targets-group-title {
  margin: 0;
  padding: 0 var(--space-1);
  color: var(--meta);
  font-size: var(--text-xs);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.term-targets-list {
  display: flex;
  flex-direction: column;
  gap: calc(var(--space-1) / 2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.term-target-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  width: 100%;
  min-height: var(--space-8);
  padding: var(--space-1) var(--space-2);
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.term-target-option:hover {
  border-color: var(--border);
  background: var(--surface-warm);
}

.term-target-option:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.term-target-option.is-container {
  background: color-mix(in oklab, var(--surface) 92%, var(--accent));
}

.term-target-option.is-pending {
  opacity: 0.72;
  cursor: progress;
}

.term-target-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-2);
  font-size: var(--text-sm);
}

.term-target-meta {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-targets-partial {
  margin: 0;
  padding: var(--space-1);
  color: var(--warn);
  font-size: var(--text-xs);
}

.term-targets-partial-item {
  display: block;
  color: var(--muted);
}

.term-targets-truncated {
  margin: 0;
  padding: var(--space-1);
  color: var(--muted);
  font-size: var(--text-xs);
}
</style>
