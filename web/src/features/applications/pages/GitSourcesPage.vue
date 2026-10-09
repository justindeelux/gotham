<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSpace,
  NSpin,
  NTable,
  NTag,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeGitHubAppError } from "@/features/applications/api/githubApp";
import { describeProviderError } from "@/features/applications/api/providers";
import GitLabConnectDialog from "@/features/applications/components/GitLabConnectDialog.vue";
import GitSourceDisconnectDialog from "@/features/applications/components/GitSourceDisconnectDialog.vue";
import {
  useGitSourcesPage,
} from "@/features/applications/composables/useGitSourcesPage";
import type { GitSourceRow } from "@/features/applications/composables/useGitSourcesPage";
import { useTeamsStore } from "@/features/teams";
import { formatDate } from "@/shared/utils/format";

/**
 * Git sources management (GS-10, JUS-66): the Settings page listing every
 * connected Git source. GitHub App connections and GitLab connections are
 * managed here through the GS-5/GS-6 automatic flows; legacy OAuth rows list
 * read-only. Connections belong to the signed-in account and the API guards
 * mutations by token scope, so the page shows its actions to every
 * authenticated user instead of gating on team role. Secrets never render:
 * the admin token and client secrets are write-only dialog fields, cleared
 * on submit.
 */
const { t } = useI18n();
const message = useMessage();
const teamsStore = useTeamsStore();
const page = useGitSourcesPage();

const showGitHub = ref(false);
const githubName = ref("gotham");
const githubWorking = ref(false);
const githubError = ref<string | null>(null);
const showGitLab = ref(false);
const disconnectRow = ref<GitSourceRow | null>(null);
const disconnectWorking = ref(false);

/** githubNameValid gates the manifest start on a non-empty name. */
const githubNameValid = computed<boolean>(() => githubName.value.trim() !== "");

/** eyebrow shows the team name when one is selected. */
const eyebrow = computed<string>(() => {
  const team = teamsStore.activeTeam?.name ?? "";
  return team === ""
    ? t("applications.gitSources.eyebrow")
    : t("applications.gitSources.eyebrowTeam", { team });
});

/** connectedCount/attentionCount summarize the table header. */
const connectedCount = computed<number>(
  () => page.rows.value.filter((row) => row.connected).length,
);
const attentionCount = computed<number>(
  () => page.rows.value.length - connectedCount.value,
);

/** rowName renders the connection name for messages. */
function rowName(row: GitSourceRow): string {
  return row.kind === "github-app"
    ? `${row.title} ${row.subtitle}`.trim()
    : `${row.title} ${row.account}`.trim();
}

/** appsCell renders the Applications column for the usage lookup state. */
function appsCell(row: GitSourceRow): string {
  if (page.usageLoading.value) {
    return t("applications.gitSources.appsPending");
  }
  if (!page.usageKnown.value) {
    return t("applications.gitSources.appsUnknown");
  }
  return row.apps.length === 0 ? t("applications.gitSources.appsNone") : String(row.apps.length);
}

onMounted(() => {
  void page.refresh().catch(() => undefined);
});

watch(
  () => teamsStore.activeTeamId,
  () => {
    void page.refresh().catch(() => undefined);
  },
);

/** openGitHub resets the name draft and its error. */
function openGitHub(): void {
  githubName.value = "gotham";
  githubError.value = null;
  showGitHub.value = true;
}

/** submitGitHub starts the manifest flow; the browser leaves for the host. */
async function submitGitHub(): Promise<void> {
  if (!githubNameValid.value || githubWorking.value) {
    return;
  }
  githubWorking.value = true;
  githubError.value = null;
  try {
    await page.connectGitHub(githubName.value.trim());
    showGitHub.value = false;
  } catch (error) {
    githubError.value = describeGitHubAppError(error);
  } finally {
    githubWorking.value = false;
  }
}

/** reconnectRow resumes the Install/Authorize step; the browser leaves. */
async function reconnectRow(row: GitSourceRow): Promise<void> {
  try {
    await page.reconnect(row);
  } catch (error) {
    message.error(
      row.kind === "github-app" ? describeGitHubAppError(error) : describeProviderError(error),
    );
  }
}

/** askDisconnect opens the confirmation listing the using applications. */
function askDisconnect(row: GitSourceRow): void {
  page.disconnectError.value = null;
  page.disconnectNames.value = [];
  disconnectRow.value = row;
}

/** confirmDisconnect forgets the connection. Failures (including the 409
 * naming the applications) stay in page.disconnectError with the dialog
 * open; the rejection is handled here, never unhandled. */
async function confirmDisconnect(): Promise<void> {
  const row = disconnectRow.value;
  if (row === null || disconnectWorking.value) {
    return;
  }
  disconnectWorking.value = true;
  try {
    const result = await page.disconnect(row);
    disconnectRow.value = null;
    if (row.kind === "github-app" && result.applicationsUsing > 0) {
      message.success(
        t("applications.gitSources.disconnectedWithApps", {
          name: rowName(row),
          count: result.applicationsUsing,
        }),
      );
    } else {
      message.success(t("applications.gitSources.disconnected", { name: rowName(row) }));
    }
  } catch {
    // Surfaced through page.disconnectError inside the open dialog.
  } finally {
    disconnectWorking.value = false;
  }
}
</script>

<template>
  <div class="git-sources-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ eyebrow }}</p>
        <h1>{{ t("applications.gitSources.title") }}</h1>
        <p class="page-desc">{{ t("applications.gitSources.description") }}</p>
      </div>
      <div class="page-actions">
        <NButton type="primary" @click="openGitHub">
          {{ t("applications.gitSources.connectGitHub") }}
        </NButton>
        <NButton @click="showGitLab = true">
          {{ t("applications.gitSources.connectGitLab") }}
        </NButton>
      </div>
    </div>

    <NAlert
      v-if="page.error.value"
      type="error"
      :show-icon="true"
      data-testid="git-sources-error"
    >
      <NSpace align="center" :size="12">
        <span>{{ page.error.value }}</span>
        <NButton size="small" @click="() => void page.refresh()">
          {{ t("applications.gitSources.retry") }}
        </NButton>
      </NSpace>
    </NAlert>

    <NSpin :show="page.loading.value" :aria-label="t('applications.gitSources.loading')">
      <NCard v-if="!page.loading.value && page.rows.value.length === 0 && !page.error.value">
        <NEmpty :description="t('applications.gitSources.emptyTitle')">
          <template #extra>
            <p class="empty-hint">{{ t("applications.gitSources.emptyHint") }}</p>
            <p class="empty-hint">{{ t("applications.gitSources.connectHint") }}</p>
            <NSpace justify="center" :size="12">
              <NButton type="primary" @click="openGitHub">
                {{ t("applications.gitSources.connectGitHub") }}
              </NButton>
              <NButton @click="showGitLab = true">
                {{ t("applications.gitSources.connectGitLab") }}
              </NButton>
            </NSpace>
          </template>
        </NEmpty>
      </NCard>

      <div v-else-if="page.rows.value.length > 0" class="table-card" data-testid="git-sources-table">
        <div class="table-head">
          <h2>{{ t("applications.gitSources.connectionsTitle") }}</h2>
          <span class="table-meta">{{
            t("applications.gitSources.connectionsSummary", {
              connected: connectedCount,
              attention: attentionCount,
            })
          }}</span>
        </div>
        <NTable :bordered="true" :single-line="false">
          <thead>
            <tr>
              <th>{{ t("applications.gitSources.tableSource") }}</th>
              <th>{{ t("applications.gitSources.tableStatus") }}</th>
              <th>{{ t("applications.gitSources.tableAccount") }}</th>
              <th>{{ t("applications.gitSources.tableInstallations") }}</th>
              <th>{{ t("applications.gitSources.tableRepos") }}</th>
              <th>{{ t("applications.gitSources.tableApps") }}</th>
              <th>{{ t("applications.gitSources.tableConnected") }}</th>
              <th>{{ t("applications.gitSources.tableActions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in page.rows.value" :key="`${row.kind}/${row.id}`">
              <td>
                <span class="cell-main">{{ row.title }}</span>
                <span class="cell-sub">{{ row.subtitle }}</span>
              </td>
              <td>
                <NTag :type="row.connected ? 'success' : 'warning'" size="small">
                  {{
                    row.connected
                      ? t("applications.gitSources.connected")
                      : t("applications.gitSources.needsAttention")
                  }}
                </NTag>
                <span v-if="!row.connected && row.kind === 'github-app'" class="cell-sub">
                  {{ t("applications.gitSources.waitingInstall") }}
                </span>
              </td>
              <td>
                <span class="cell-main">{{ row.account }}</span>
                <span class="cell-sub">{{ row.instance }}</span>
              </td>
              <td>{{ row.installations === null ? "—" : row.installations }}</td>
              <td>{{ row.repos === null ? t("applications.gitSources.reposUnknown") : row.repos }}</td>
              <td>{{ appsCell(row) }}</td>
              <td class="mono">{{ formatDate(row.createdAt) }}</td>
              <td>
                <NSpace v-if="!row.legacy" :size="8" :wrap="true">
                  <NButton
                    v-if="!row.connected"
                    size="small"
                    type="primary"
                    @click="void reconnectRow(row)"
                  >
                    {{ row.kind === "github-app" ? t("applications.gitSources.install") : t("applications.gitSources.reconnect") }}
                  </NButton>
                  <NButton v-else size="small" @click="void reconnectRow(row)">
                    {{ t("applications.gitSources.reconnect") }}
                  </NButton>
                  <NButton size="small" type="error" @click="askDisconnect(row)">
                    {{ t("applications.gitSources.disconnect") }}
                  </NButton>
                </NSpace>
                <span v-else class="cell-sub">{{ t("applications.gitSources.legacyManaged") }}</span>
              </td>
            </tr>
          </tbody>
        </NTable>
      </div>
    </NSpin>

    <NModal
      :show="showGitHub"
      preset="card"
      :title="t('applications.gitSources.connectGitHub')"
      class="dialog-card connect-modal"
      style="width: 520px; max-width: 94vw"
      :mask-closable="false"
      @update:show="(value: boolean) => { showGitHub = value; }"
    >
      <p class="dialog-hint">{{ t("applications.gitSources.githubConnectHint") }}</p>
      <NForm label-placement="top">
        <NFormItem
          :label="t('applications.gitSources.githubName')"
          :label-props="{ for: 'github-app-name-input' }"
        >
          <NInput
            v-model:value="githubName"
            class="mono"
            :input-props="{ id: 'github-app-name-input' }"
            :placeholder="t('applications.gitSources.githubNamePlaceholder')"
          />
          <template #feedback>
            <span class="field-hint">{{ t("applications.gitSources.githubNameHint") }}</span>
          </template>
        </NFormItem>
        <NAlert v-if="githubError" type="error" :show-icon="true">{{ githubError }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace :size="12" justify="end">
          <NButton :disabled="githubWorking" @click="showGitHub = false">
            {{ t("applications.gitSources.cancel") }}
          </NButton>
          <NButton
            type="primary"
            :disabled="!githubNameValid"
            :loading="githubWorking"
            @click="void submitGitHub()"
          >
            {{ t("applications.gitSources.connectGitHub") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <GitLabConnectDialog v-model:show="showGitLab" @connected="() => void page.refresh()" />

    <GitSourceDisconnectDialog
      :show="disconnectRow !== null"
      :row="disconnectRow"
      :working="disconnectWorking"
      :usage-known="page.usageKnown.value"
      :blocked-names="page.disconnectNames.value"
      :blocked-error="page.disconnectError.value ?? ''"
      @update:show="(value: boolean) => { if (!value) disconnectRow = null; }"
      @confirm="void confirmDisconnect()"
    />
  </div>
</template>

<style scoped>
.git-sources-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.table-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.table-head {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.table-head h2 {
  font-size: var(--text-lg);
  color: var(--fg-2);
  margin: 0;
}

.table-meta {
  font-size: var(--text-xs);
  color: var(--meta);
}

.cell-main {
  display: block;
  color: var(--fg-2);
  font-weight: 500;
}

.cell-sub {
  display: block;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}

.empty-hint {
  color: var(--muted);
  margin: 0 0 var(--space-3);
}

.dialog-hint {
  margin: 0 0 var(--space-3);
  color: var(--muted);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
  }
}
</style>

<!--
  Unscoped on purpose: NModal teleports the card to <body>, so scoped
  selectors (which compile to a [data-v] ancestor match) never reach it.
  Every rule stays behind the .connect-modal class owned by this dialog.
-->
<style>
.connect-modal.n-modal.n-card {
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
}

.connect-modal.n-modal.n-card > .n-card-content {
  overflow-y: auto;
  min-height: 0;
}

.connect-modal .n-form-item-blank {
  display: block;
}

.connect-modal .n-form-item-blank > .n-input {
  width: 100%;
}
</style>
