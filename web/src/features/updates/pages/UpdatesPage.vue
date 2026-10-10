<script setup lang="ts">
import { NAlert, NButton, NCard, NCollapse, NCollapseItem, NEmpty, NIcon, NProgress, NSpace, NSpin, NTag, useDialog } from "naive-ui";
import { Download, RefreshCw } from "@lucide/vue";
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useUpdates } from "@/features/updates/composables/useUpdates";
import UpdateScheduleForm from "@/features/updates/components/UpdateScheduleForm.vue";
import { renderMarkdown } from "@/features/updates/markdown";
import type { ChangelogEntry } from "@/features/updates/schemas/updates";
import { formatDate, relativeTime } from "@/shared/utils/format";

/**
 * Settings → Updates (JUS-93, changelog JUS-102): version / channel / latest
 * release, check and update actions with progress and failure state, the
 * persisted check / auto-apply schedule, and the admin-gated changelog viewer
 * (sanitized markdown, GitHub link, intermediate versions newest first, the
 * running version's entry when up to date, empty state for dev builds).
 * Mockup: docs/design/updates.html. Non-admins see status only.
 */
const { t } = useI18n();
const dialog = useDialog();
const u = useUpdates();

const latest = computed(() => (u.check.value?.available ? u.check.value.version : undefined));
const channel = computed(() => u.state.value?.schedule.channel ?? "stable");
const last = computed(() => u.check.value?.last_update);
const failed = computed(
  () => !!last.value && ["rolled_back", "rollback_failed", "no_backup", "wrapper_failed"].includes(last.value.result),
);
const lastKey = computed(() => (last.value ? `updates.last.${last.value.result}` : ""));
const nextCheck = computed(() =>
  u.state.value?.next_run_at
    ? t("updates.stats.nextCheck", { time: relativeTime(u.state.value.next_run_at) })
    : "",
);
const backoffNote = computed(() => {
  const backoff = u.state.value?.backoff;
  if (!backoff) {
    return "";
  }
  return t("updates.errors.backoff", {
    version: backoff.version,
    time: `${formatDate(backoff.until)} ${new Date(backoff.until).toLocaleTimeString()}`,
  });
});

/** Changelog entries newest first (the API already orders them). */
const changelogEntries = computed(() => u.changelog.value?.entries ?? []);
const expandedChangelog = computed(() =>
  changelogEntries.value.length > 0 ? [changelogEntries.value[0].version] : [],
);
/** Single entry matching the running version means "up to date": title it so. */
function entryTitle(entry: ChangelogEntry): string {
  if (changelogEntries.value.length === 1 && entry.version === u.check.value?.current) {
    return t("updates.changelog.currentTitle", { version: entry.version });
  }
  return entry.version;
}
/** Sanitized HTML for one release body (renderMarkdown escapes raw HTML). */
function renderedNotes(entry: ChangelogEntry): string {
  return renderMarkdown(entry.notes ?? "", entry.html_url ?? "");
}

function confirmUpdate(): void {
  dialog.warning({
    title: t("updates.confirm.title", { version: latest.value ?? "" }),
    content: t("updates.confirm.body"),
    positiveText: t("updates.actions.update"),
    negativeText: t("updates.actions.cancel"),
    onPositiveClick: () => {
      void u.startUpdate();
    },
  });
}

onMounted(() => {
  void u.load();
});
</script>

<template>
  <div class="updates-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("updates.page.eyebrow") }}</p>
        <h1>{{ t("updates.page.title") }}</h1>
        <p class="page-desc">{{ t("updates.page.description") }}</p>
      </div>
    </div>

    <NCard v-if="u.unavailable.value">
      <NEmpty :description="t('updates.page.unavailable')" />
    </NCard>

    <NSpin v-else :show="u.checking.value && !u.check.value">
      <NSpace vertical :size="16">
        <NAlert v-if="u.checkError.value" type="error" data-testid="check-error">
          {{ t("updates.errors.check", { message: u.checkError.value }) }}
        </NAlert>
        <NAlert v-if="u.applyError.value" type="error" data-testid="apply-error">
          {{ t("updates.errors.apply", { message: u.applyError.value }) }}
        </NAlert>

        <NCard v-if="u.check.value" data-testid="update-stats">
          <div class="stat-grid">
            <div class="stat">
              <div class="stat-label">{{ t("updates.stats.current") }}</div>
              <div class="stat-value">{{ u.check.value.current }}</div>
              <div class="stat-sub"><NTag size="small">{{ t(`updates.stats.channel.${channel}`) }}</NTag></div>
            </div>
            <div class="stat">
              <div class="stat-label">{{ t("updates.stats.latest") }}</div>
              <div class="stat-value">{{ latest ?? u.check.value.current }}</div>
              <div class="stat-sub">
                <NTag size="small" :type="latest ? 'info' : 'success'">
                  {{ latest ? t("updates.stats.updateAvailable") : t("updates.stats.upToDate") }}
                </NTag>
                <span v-if="u.check.value.published_at">{{ formatDate(u.check.value.published_at) }}</span>
              </div>
            </div>
            <div class="stat">
              <div class="stat-label">{{ t("updates.stats.lastChecked") }}</div>
              <div class="stat-value">
                {{ u.state.value?.last_checked_at ? relativeTime(u.state.value.last_checked_at) : t("updates.stats.notChecked") }}
              </div>
              <div class="stat-sub">{{ nextCheck }}</div>
            </div>
          </div>
          <div v-if="u.isAdmin.value" class="stat-actions">
            <NButton :loading="u.checking.value" :disabled="u.awaitingRestart.value" @click="u.runCheck()">
              <template #icon><NIcon><RefreshCw /></NIcon></template>
              {{ t("updates.actions.check") }}
            </NButton>
            <NButton
              type="primary"
              :disabled="!latest || u.applying.value || u.awaitingRestart.value"
              data-testid="update-now"
              @click="confirmUpdate"
            >
              <template #icon><NIcon><Download /></NIcon></template>
              {{ t("updates.actions.update") }}
            </NButton>
          </div>
        </NCard>

        <NCard v-if="u.awaitingRestart.value" :title="t('updates.progress.title')" data-testid="update-progress">
          <template #header-extra>
            <span class="meta">{{ t("updates.progress.installing", { version: latest ?? "" }) }}</span>
          </template>
          <NProgress type="line" :percentage="60" processing :show-indicator="false" />
          <p class="meta">{{ t("updates.progress.restarting") }}</p>
        </NCard>

        <NCard v-if="last && !u.awaitingRestart.value" :title="t('updates.last.title')" data-testid="last-update">
          <template #header-extra>
            <NTag :type="last.result === 'ok' ? 'success' : failed ? 'error' : 'warning'" size="small">
              {{ t(lastKey) }}
            </NTag>
          </template>
          <p class="meta">
            {{ last.version }} <template v-if="last.at">· {{ formatDate(last.at) }}</template>
          </p>
          <NAlert v-if="failed" type="error" data-testid="update-failed">
            {{ last.detail ? t("updates.last.detail", { detail: last.detail }) : t(lastKey) }}
          </NAlert>
          <NAlert v-if="backoffNote" type="warning" class="backoff" data-testid="update-backoff">
            {{ backoffNote }}
          </NAlert>
        </NCard>

        <NCard
          v-if="u.isAdmin.value && (u.changelog.value || u.changelogLoading.value)"
          :title="t('updates.changelog.title')"
          data-testid="changelog"
        >
          <NSpin :show="u.changelogLoading.value && !u.changelog.value">
            <NEmpty
              v-if="u.changelog.value && changelogEntries.length === 0"
              :description="t('updates.changelog.empty')"
            />
            <NCollapse
              v-else-if="changelogEntries.length > 0"
              :default-expanded-names="expandedChangelog"
            >
              <NCollapseItem
                v-for="entry in changelogEntries"
                :key="entry.version"
                :name="entry.version"
                :title="entryTitle(entry)"
              >
                <template #header-extra>
                  <a
                    v-if="entry.html_url"
                    :href="entry.html_url"
                    target="_blank"
                    rel="noopener noreferrer"
                    @click.stop
                  >
                    {{ t("updates.changelog.github") }}
                  </a>
                </template>
                <div v-if="entry.notes" class="notes-rendered" v-html="renderedNotes(entry)" />
                <NEmpty v-else :description="t('updates.notes.empty')" />
                <p v-if="entry.published_at" class="meta">{{ formatDate(entry.published_at) }}</p>
              </NCollapseItem>
            </NCollapse>
          </NSpin>
        </NCard>

        <UpdateScheduleForm
          v-if="u.state.value"
          :state="u.state.value"
          :can-edit="u.isAdmin.value"
          :saving="u.saving.value"
          :error="u.saveError.value"
          :save="u.saveSchedule"
        />
      </NSpace>
    </NSpin>
  </div>
</template>

<style scoped>
.updates-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--space-4);
}

.stat-label {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
}

.stat-value {
  margin-top: 6px;
  font-family: var(--font-display);
  font-size: var(--text-2xl);
  font-weight: 700;
  line-height: 1.1;
  letter-spacing: -0.02em;
  color: var(--fg-2);
}

.stat-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  font-size: var(--text-xs);
  color: var(--muted);
}

.stat-actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}

.notes-rendered {
  font-size: var(--text-sm);
  max-height: 320px;
  overflow: auto;
}

.notes-rendered :is(h4, h5, h6) {
  margin: 0 0 6px;
}

.notes-rendered ul,
.notes-rendered ol {
  margin: 0 0 10px;
  padding-left: 20px;
}

.notes-rendered code {
  font-family: var(--font-mono);
  font-size: 12px;
}

.backoff {
  margin-top: var(--space-3);
}
</style>
