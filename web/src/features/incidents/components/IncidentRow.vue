<script setup lang="ts">
import { NButton, NDropdown, NTag, NText } from "naive-ui";
import { computed, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { getApplication } from "@/features/applications";
import { getDatabase } from "@/features/databases";
import { useAuthStore } from "@/features/auth";
import type { Incident } from "@/features/incidents/api/incidents";
import { useIncidentsStore } from "@/features/incidents/stores/incidents";
import {
  coverageKey,
  deliveryHold,
  elapsedLabel,
  isKnownResourceType,
  reasonKey,
  resourceName,
  resourceTypeKey,
  severityKey,
  severityTagType,
} from "@/features/incidents/utils/incidents";
import { getService } from "@/features/services/api/services";
import { formatDateTime, relativeTime } from "@/shared/utils/format";

interface Props {
  incident: Incident;
  nested?: boolean;
}

const props = withDefaults(defineProps<Props>(), { nested: false });

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const store = useIncidentsStore();

const busy = ref(false);
const pending = ref(false);
const actionFailed = ref(false);
const linkFailed = ref(false);

const severityLabel = computed<string>(() => t(severityKey(props.incident.severity)));
const pendingLabel = computed<string>(() => t("incidents.lifecycle.pending"));
const reason = computed<string>(() => t(reasonKey(props.incident.detector)));
const typeLabel = computed<string>(() => t(resourceTypeKey(props.incident.resource_type)));
const name = computed<string>(() =>
  isKnownResourceType(props.incident.resource_type)
    ? resourceName(props.incident, t("incidents.unknownResourceName"))
    : t("incidents.unknownResourceName"),
);
const started = computed<string>(() =>
  t("incidents.started", {
    time: relativeTime(props.incident.first_observed_at, null, { long: true }),
  }),
);
const lastValid = computed<string>(() =>
  props.incident.last_valid_at
    ? t("incidents.lastValid", {
        time: relativeTime(props.incident.last_valid_at, null, { long: true }),
      })
    : t("incidents.noValid"),
);
const coverage = computed<string>(() => t(coverageKey(props.incident.coverage)));
const elapsed = computed<string>(() => elapsedLabel(props.incident.first_observed_at));
const acknowledged = computed<boolean>(() => Boolean(props.incident.acknowledged_at));
const snoozedUntil = computed<string>(() => {
  const until = props.incident.snoozed_until;
  if (!until || Date.parse(until) <= Date.now()) {
    return "";
  }
  return t("incidents.status.snoozed", { time: formatDateTime(until) });
});
const hold = computed(() => deliveryHold(props.incident.delivery_hold));

type LinkKind = "application" | "database" | "service" | "server" | "metrics";

const link = computed<{ kind: LinkKind; label: string } | null>(() => {
  if (!props.incident.resource_id) {
    return null;
  }
  switch (props.incident.resource_type) {
    case "application":
      return { kind: "application", label: t("incidents.actions.open") };
    case "database":
      return { kind: "database", label: t("incidents.actions.open") };
    case "service":
      return { kind: "service", label: t("incidents.actions.open") };
    case "node":
      return authStore.isOwner
        ? { kind: "server", label: t("incidents.actions.server") }
        : null;
    case "filesystem":
      return authStore.isOwner
        ? { kind: "metrics", label: t("incidents.actions.metrics") }
        : null;
    default:
      return null;
  }
});

const logsLink = computed<boolean>(
  () =>
    props.incident.resource_type === "application" &&
    props.incident.detector.startsWith("runtime_"),
);

const snoozeOptions = computed(() => [
  { label: t("incidents.actions.snoozeHour"), key: "1" },
  { label: t("incidents.actions.snoozeDay"), key: "24" },
]);

function serverRoute(): { name: string; params: { id: string }; query: { tab: string } } | null {
  const id = props.incident.node_id || props.incident.resource_id;
  if (!id) {
    return null;
  }
  return { name: "server-detail", params: { id }, query: { tab: "metrics" } };
}

async function openResource(logs = false): Promise<void> {
  if (!link.value || pending.value) {
    return;
  }
  linkFailed.value = false;
  if (link.value.kind === "server" || link.value.kind === "metrics") {
    const route = serverRoute();
    if (route) {
      await router.push(route);
    }
    return;
  }
  const id = props.incident.resource_id ?? "";
  pending.value = true;
  try {
    if (link.value.kind === "application") {
      const application = await getApplication(id);
      await router.push({
        name: "application-detail",
        params: {
          projectId: application.project_id,
          environmentId: application.environment_id,
          id: application.id,
        },
        query: logs ? { tab: "logs" } : undefined,
      });
    } else if (link.value.kind === "database") {
      const database = await getDatabase(id);
      await router.push({
        name: "database-detail",
        params: {
          projectId: database.project_id,
          environmentId: database.environment_id,
          id: database.id,
        },
      });
    } else {
      const service = await getService(id);
      await router.push({
        name: "service-detail",
        params: {
          projectId: service.project_id,
          environmentId: service.environment_id,
          id: service.id,
        },
      });
    }
  } catch {
    linkFailed.value = true;
  } finally {
    pending.value = false;
  }
}

async function acknowledge(): Promise<void> {
  if (busy.value) {
    return;
  }
  busy.value = true;
  actionFailed.value = false;
  try {
    await store.acknowledge(props.incident.id);
  } catch {
    actionFailed.value = true;
  } finally {
    busy.value = false;
  }
}

async function snooze(key: string): Promise<void> {
  if (busy.value) {
    return;
  }
  busy.value = true;
  actionFailed.value = false;
  try {
    await store.snooze(props.incident.id, key === "24" ? 24 : 1);
  } catch {
    actionFailed.value = true;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="incident-row" :class="{ 'is-nested': nested }">
    <div class="incident-head">
      <NTag
        size="small"
        :type="severityTagType(incident.severity)"
        :bordered="false"
      >
        {{ severityLabel }}
      </NTag>
      <NTag v-if="incident.lifecycle === 'pending'" size="small" :bordered="false">
        {{ pendingLabel }}
      </NTag>
      <span class="incident-reason">{{ reason }}</span>
    </div>

    <p class="incident-resource">
      <span class="incident-type">{{ typeLabel }}</span>
      <span aria-hidden="true">·</span>
      <span class="incident-name" :title="name">{{ name }}</span>
    </p>

    <div class="incident-meta">
      <span>{{ started }}</span>
      <span v-if="incident.lifecycle !== 'resolved'">
        {{ t("incidents.duration", { duration: elapsed }) }}
      </span>
      <span>{{ lastValid }}</span>
      <NTag size="small" :bordered="false">{{ coverage }}</NTag>
    </div>

    <div
      v-if="acknowledged || snoozedUntil || hold.kind === 'known'"
      class="incident-status"
    >
      <NTag v-if="acknowledged" size="small" type="success" :bordered="false">
        {{ t("incidents.status.acknowledged") }}
      </NTag>
      <NTag v-if="snoozedUntil" size="small" type="info" :bordered="false">
        {{ snoozedUntil }}
      </NTag>
      <NText v-if="hold.kind === 'known'" depth="3" class="incident-hold">
        {{ t(hold.key) }}
        <RouterLink :to="{ name: 'notifications' }">
          {{ t("incidents.hold.settings") }}
        </RouterLink>
      </NText>
    </div>

    <p v-if="actionFailed" class="incident-error" role="alert">
      {{ t("incidents.error.body") }}
    </p>
    <p v-if="linkFailed" class="incident-error" role="alert">
      {{ t("notifications.inbox.unavailable") }}
    </p>

    <div
      class="incident-actions"
      role="group"
      :aria-label="t('incidents.actionsFor', { name })"
    >
      <NButton
        v-if="!acknowledged"
        size="small"
        :loading="busy"
        @click="acknowledge"
      >
        {{ t("incidents.actions.acknowledge") }}
      </NButton>
      <NDropdown
        trigger="click"
        :options="snoozeOptions"
        @select="snooze"
      >
        <NButton size="small" :disabled="busy" :aria-label="t('incidents.snoozeMenu')">
          {{ t("incidents.actions.snooze") }}
        </NButton>
      </NDropdown>
      <NButton
        v-if="link"
        size="small"
        quaternary
        :loading="pending"
        @click="openResource()"
      >
        {{ link.label }}
      </NButton>
      <NButton
        v-if="logsLink"
        size="small"
        quaternary
        :loading="pending"
        @click="openResource(true)"
      >
        {{ t("incidents.actions.logs") }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.incident-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) 0;
  min-width: 0;
}

.incident-row.is-nested {
  padding: var(--space-2) 0 var(--space-2) var(--space-4);
  border-left: 2px solid var(--border-soft);
}

.incident-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  min-width: 0;
}

.incident-reason {
  color: var(--fg);
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}

.incident-resource {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  margin: 0;
  min-width: 0;
  color: var(--fg-2);
  font-size: var(--text-sm);
}

.incident-type {
  color: var(--muted);
  flex: 0 0 auto;
}

.incident-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
  font-family: var(--font-mono);
}

.incident-meta {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  font-size: var(--text-xs);
  color: var(--muted);
}

.incident-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.incident-hold {
  font-size: var(--text-xs);
}

.incident-error {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--danger-ink);
}

.incident-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

@media (max-width: 480px) {
  .incident-actions :deep(.n-button) {
    flex: 1 1 auto;
  }
}
</style>
