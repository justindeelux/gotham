<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSelect,
  NSkeleton,
  NTag,
  NText,
} from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import IncidentRow from "@/features/incidents/components/IncidentRow.vue";
import type { Incident, IncidentSeverity } from "@/features/incidents/api/incidents";
import { useIncidentsStore } from "@/features/incidents/stores/incidents";
import {
  reasonKey,
  resourceName,
  resourceTypeKey,
  severityKey,
  severityTagType,
} from "@/features/incidents/utils/incidents";
import { groupIncidents } from "@/features/incidents/utils/incidents";
import DeliveryFailuresList from "@/features/notifications/components/DeliveryFailuresList.vue";
import { useNotificationInboxStore } from "@/features/notifications/stores/inbox";
import { useServersStore } from "@/features/servers";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { relativeTime } from "@/shared/utils/format";

const { t } = useI18n();
const authStore = useAuthStore();
const store = useIncidentsStore();
const inbox = useNotificationInboxStore();
const serversStore = useServersStore();

const showResolved = ref(false);
const groups = computed(() => groupIncidents(store.items));

const activeCountText = computed<string>(() =>
  store.activeCount === 1
    ? t("incidents.countOne", { count: 1 })
    : t("incidents.countOther", { count: store.activeCount }),
);

const emptyTitle = computed<string>(() => {
  if (hasFilters.value) {
    return t("incidents.filtered.title");
  }
  if (noMonitoringData.value) {
    return t("incidents.noData.title");
  }
  return t("incidents.healthy.title");
});

const emptyBody = computed<string>(() => {
  if (hasFilters.value) {
    return t("incidents.filtered.body");
  }
  return noMonitoringData.value
    ? t("incidents.noData.body")
    : t("incidents.healthy.body");
});

const hasFilters = computed<boolean>(
  () => store.severity !== "" || store.resourceType !== "",
);

// No server has ever reported, so "healthy" would overstate what Gotham knows.
const noMonitoringData = computed<boolean>(
  () => authStore.isOwner && serversStore.servers.length === 0,
);

const emptyState = computed<boolean>(
  () =>
    store.state === "ready" &&
    store.items.length === 0 &&
    !store.loading,
);

const compact = computed<boolean>(
  () => emptyState.value && !hasFilters.value,
);

const severityOptions = computed(() => [
  { label: t("incidents.filters.allSeverities"), value: "" },
  { label: t("incidents.severity.critical"), value: "critical" },
  { label: t("incidents.severity.warning"), value: "warning" },
]);

const resourceOptions = computed(() => {
  const types = new Set([
    "application",
    "database",
    "service",
    "node",
    "filesystem",
    "domain",
  ]);
  for (const incident of store.items) {
    if (incident.resource_type) {
      types.add(incident.resource_type);
    }
  }
  const options = Array.from(types)
    .sort()
    .map((type) => ({
      label: t(resourceTypeKey(type)),
      value: type,
    }));
  return [{ label: t("incidents.filters.allResources"), value: "" }, ...options];
});

const affectedText = (count: number): string =>
  count === 1
    ? t("incidents.affectedOne", { count: 1 })
    : t("incidents.affectedOther", { count });

function severityOf(incident: Incident): string {
  return t(severityKey(incident.severity));
}

function resolvedName(incident: Incident): string {
  return resourceName(incident, t("incidents.unknownResourceName"));
}

function onSeverity(value: string): void {
  store.setSeverity(value as IncidentSeverity | "");
}

function onResource(value: string): void {
  store.setResourceType(value);
}

async function toggleResolved(): Promise<void> {
  showResolved.value = !showResolved.value;
  if (showResolved.value) {
    await store.refreshResolved();
  }
}

function onRealtimeChange(): void {
  void store.refresh();
  if (store.resolvedLoaded) {
    void store.refreshResolved();
  }
}

watch(() => inbox.revision, onRealtimeChange);

onMounted(() => {
  store.start();
});

onUnmounted(() => {
  store.stop();
});
</script>

<template>
  <section class="attention" aria-labelledby="attention-title">
    <div class="section-title">
      <h2 id="attention-title">{{ t("incidents.title") }}</h2>
      <NTag
        v-if="store.state === 'ready'"
        size="small"
        :bordered="false"
        :type="store.activeCount > 0 ? 'error' : 'success'"
      >
        {{ activeCountText }}
      </NTag>
      <NText v-if="!compact" depth="3" class="meta">{{ t("incidents.subtitle") }}</NText>
      <NText v-else depth="3" class="meta">{{ emptyTitle }}</NText>
      <NButton
        class="resolved-toggle"
        quaternary
        size="small"
        :aria-expanded="showResolved"
        @click="toggleResolved"
      >
        {{ t("incidents.resolved.title") }}
      </NButton>
    </div>

    <NCard v-if="!compact" size="small">
      <NAlert
        v-if="store.state === 'error'"
        type="error"
        :show-icon="true"
        data-testid="incidents-error"
      >
        <template #header>{{ t("incidents.error.title") }}</template>
        {{ t("incidents.error.body") }}
        <div class="alert-action">
          <NButton size="small" @click="() => void store.refresh()">
            {{ t("incidents.error.retry") }}
          </NButton>
        </div>
      </NAlert>
      <NAlert
        v-else-if="store.state === 'unavailable'"
        type="warning"
        :show-icon="true"
        data-testid="incidents-unavailable"
      >
        <template #header>{{ t("incidents.unavailable.title") }}</template>
        {{ t("incidents.unavailable.body") }}
      </NAlert>

      <NSkeleton
        v-if="store.loading && store.items.length === 0"
        text
        :repeat="3"
        class="loading"
      />

      <div
        v-if="store.state === 'ready' && (store.items.length > 0 || hasFilters)"
        class="attention-filters"
      >
        <div class="filter">
          <span class="filter-label">{{ t("incidents.filters.severity") }}</span>
          <NSelect
            size="small"
            :value="store.severity"
            :options="severityOptions"
            :input-props="{ 'aria-label': t('incidents.filters.severity') }"
            @update:value="onSeverity"
          />
        </div>
        <div class="filter">
          <span class="filter-label">{{ t("incidents.filters.resource") }}</span>
          <NSelect
            size="small"
            :value="store.resourceType"
            :options="resourceOptions"
            :input-props="{ 'aria-label': t('incidents.filters.resource') }"
            @update:value="onResource"
          />
        </div>
      </div>

      <template v-if="store.items.length > 0">
        <ul class="incident-list">
          <li
            v-for="group in groups"
            :key="group.incident.id"
            class="incident-group"
          >
            <IncidentRow :incident="group.incident" />
            <div v-if="group.children.length > 0" class="affected">
              <NText depth="3" class="affected-title">
                {{ affectedText(group.children.length) }}
              </NText>
              <NText depth="3" class="affected-hint">
                {{ t("incidents.groupedHint") }}
              </NText>
              <ul class="incident-children">
                <li
                  v-for="child in group.children"
                  :key="child.id"
                >
                  <IncidentRow :incident="child" nested />
                </li>
              </ul>
            </div>
          </li>
        </ul>

        <div v-if="store.nextCursor" class="load-more">
          <NButton
            size="small"
            :loading="store.loadingMore"
            @click="() => void store.loadMore()"
          >
            {{ t("incidents.loadMore") }}
          </NButton>
        </div>
      </template>

      <NEmpty
        v-else-if="emptyState"
        :description="emptyTitle"
        data-testid="incidents-empty"
      >
        <template #icon>
          <GothamIcon :name="noMonitoringData ? 'server' : 'shield'" />
        </template>
        <template #extra>
          <NText depth="3">{{ emptyBody }}</NText>
        </template>
      </NEmpty>
    </NCard>

    <DeliveryFailuresList v-if="authStore.isOwner" />

    <NCard v-if="showResolved" size="small" class="resolved-card">
      <NAlert
        v-if="store.resolvedError"
        type="error"
        :show-icon="true"
      >
        {{ t("incidents.resolved.loadFailed") }}
        <div class="alert-action">
          <NButton size="small" @click="() => void store.refreshResolved()">
            {{ t("incidents.error.retry") }}
          </NButton>
        </div>
      </NAlert>
      <NSkeleton
        v-else-if="store.resolvedLoading"
        text
        :repeat="2"
      />
      <NEmpty
        v-else-if="store.resolved.length === 0"
        size="small"
        :description="t('incidents.resolved.empty')"
      />
      <ul v-else class="resolved-list">
        <li
          v-for="incident in store.resolved"
          :key="incident.id"
          class="resolved-row"
        >
          <div class="resolved-main">
            <NTag
              size="small"
              :type="severityTagType(incident.severity)"
              :bordered="false"
            >
              {{ severityOf(incident) }}
            </NTag>
            <span class="resolved-reason">{{ t(reasonKey(incident.detector)) }}</span>
            <span class="resolved-name" :title="resolvedName(incident)">
              {{ resolvedName(incident) }}
            </span>
          </div>
          <NText depth="3" class="resolved-time">
            {{
              t("incidents.resolved.at", {
                time: relativeTime(incident.resolved_at, null, { long: true }),
              })
            }}
          </NText>
        </li>
      </ul>
    </NCard>
  </section>
</template>

<style scoped>
.attention {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.section-title {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.section-title h2 {
  font-size: var(--text-xl);
  color: var(--fg-2);
  margin: 0;
}

.meta {
  font-size: var(--text-xs);
  color: var(--muted);
}

.loading {
  padding: var(--space-2) 0;
}

.alert-action {
  margin-top: var(--space-2);
}

.attention-filters {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 220px));
  gap: var(--space-3);
  margin-bottom: var(--space-2);
}

.filter {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.filter-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.incident-list,
.incident-children,
.resolved-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.incident-group + .incident-group {
  border-top: 1px solid var(--border);
}

.affected {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: 0 0 var(--space-3);
}

.affected-title {
  font-size: var(--text-xs);
  font-weight: 600;
}

.affected-hint {
  font-size: var(--text-xs);
  max-width: 64ch;
}

.incident-children {
  margin-top: var(--space-1);
}

.load-more {
  padding-top: var(--space-3);
}

.resolved-toggle {
  margin-left: auto;
}

.resolved-card {
  width: 100%;
}

.resolved-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding: var(--space-2) 0;
  min-width: 0;
}

.resolved-row + .resolved-row {
  border-top: 1px solid var(--border);
}

.resolved-main {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex-wrap: wrap;
}

.resolved-reason {
  font-size: var(--text-sm);
  color: var(--fg);
}

.resolved-name {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 28rem;
}

.resolved-time {
  font-size: var(--text-xs);
  flex: 0 0 auto;
}

@media (max-width: 640px) {
  .attention-filters {
    grid-template-columns: minmax(0, 1fr);
  }

  .resolved-row {
    align-items: flex-start;
  }
}
</style>
