<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NInputNumber,
  NSpace,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { RuntimeHealth, RuntimeHealthState, RuntimeReplicaHealth } from "@/shared/types/runtimeHealth";
import { runtimeHealthTag } from "@/shared/types/runtimeHealth";
import { relativeTime } from "@/shared/utils/format";
import {
  RUNTIME_MONITOR_DEFAULT_GRACE_SECONDS,
  RUNTIME_MONITOR_MAX_GRACE_SECONDS,
  RUNTIME_MONITOR_MIN_GRACE_SECONDS,
  isRuntimeMonitorGraceValid,
} from "@/shared/validation/monitoring";

interface Props {
  health?: RuntimeHealth | null;
  canWrite?: boolean;
  saving?: boolean;
  error?: string | null;
  showLogs?: boolean;
  showDeployments?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  health: null,
  canWrite: false,
  saving: false,
  error: null,
  showLogs: false,
  showDeployments: false,
});

const emit = defineEmits<{
  save: [value: { enabled: boolean; graceSeconds: number }];
  "view-logs": [];
  "view-deployments": [];
}>();

const { t, te } = useI18n();

const enabled = ref<boolean>(props.health?.enabled ?? true);
const grace = ref<number>(props.health?.grace_seconds ?? RUNTIME_MONITOR_DEFAULT_GRACE_SECONDS);
const dirty = ref(false);

watch(
  () => props.health,
  (value) => {
    if (!value || dirty.value) {
      return;
    }
    enabled.value = value.enabled;
    grace.value = value.grace_seconds;
  },
  { immediate: true },
);

// A successful save clears the dirty flag once its response arrives; a
// background refetch must not overwrite an edit that is still pending.
watch(
  () => props.saving,
  (saving, previous) => {
    if (previous && !saving && !props.error) {
      dirty.value = false;
    }
  },
);

const state = computed<string>(() => props.health?.state ?? "unknown");
const stateText = computed<string>(() =>
  te(`runtimeHealth.states.${state.value}`)
    ? t(`runtimeHealth.states.${state.value}`)
    : state.value,
);
const reasonText = computed<string>(() => translateReason(props.health?.reason));
const coverageText = computed<string>(() =>
  props.health?.coverage === "complete"
    ? t("runtimeHealth.coverageComplete")
    : t("runtimeHealth.coverageUnknown"),
);
const graceError = computed<string>(() =>
  isRuntimeMonitorGraceValid(grace.value) ? "" : t("runtimeHealth.graceInvalid"),
);
const affected = computed<RuntimeReplicaHealth[]>(() => props.health?.affected ?? []);

function translateReason(reason?: string): string {
  if (!reason) {
    return "";
  }
  const key = `runtimeHealth.reasons.${reason}`;
  return te(key) ? t(key) : reason;
}

function replicaLabel(replica: RuntimeReplicaHealth): string {
  const parts = [replica.service, replica.replica].filter(
    (part) => part !== undefined && part !== "",
  );
  return parts.length > 0 ? parts.join(" · ") : "—";
}

function submit(): void {
  if (graceError.value !== "") {
    return;
  }
  emit("save", { enabled: enabled.value, graceSeconds: grace.value });
}
</script>

<template>
  <NCard :title="t('runtimeHealth.title')">
    <template #header-extra>
      <NTag size="small" :type="runtimeHealthTag(state as RuntimeHealthState)" data-testid="runtime-health-state">
        {{ stateText }}
      </NTag>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="props.error" type="error" :show-icon="true">
        {{ props.error }}
      </NAlert>
      <NAlert
        v-if="state === 'unknown' || state === 'disabled'"
        type="info"
        :show-icon="true"
        data-testid="runtime-health-evidence"
      >
        {{ state === "disabled" ? t("runtimeHealth.reasons.disabled") : (reasonText || t("runtimeHealth.unknownEvidence")) }}
      </NAlert>

      <div class="summary" role="group" :aria-label="t('runtimeHealth.title')">
        <NText depth="3">{{ t("runtimeHealth.measured") }}</NText>
        <NText data-testid="runtime-health-observed">
          {{ props.health?.observed_at ? relativeTime(props.health.observed_at) : t("runtimeHealth.noEvidence") }}
        </NText>
        <NText depth="3">{{ t("runtimeHealth.coverage") }}</NText>
        <NText>{{ coverageText }}</NText>
        <template v-if="reasonText">
          <NText depth="3">{{ t("runtimeHealth.reason") }}</NText>
          <NText data-testid="runtime-health-reason">{{ reasonText }}</NText>
        </template>
      </div>

      <div v-if="affected.length > 0" class="replicas">
        <NText depth="3">{{ t("runtimeHealth.affected") }}</NText>
        <ul>
          <li
            v-for="replica in affected"
            :key="`${replica.service}:${replica.replica}:${replica.container_id}`"
            class="replica"
            data-testid="runtime-health-replica"
          >
            <span class="mono">{{ replicaLabel(replica) }}</span>
            <NTag size="small" :type="runtimeHealthTag(replica.state as RuntimeHealthState)">
              {{ te(`runtimeHealth.states.${replica.state}`) ? t(`runtimeHealth.states.${replica.state}`) : replica.state }}
            </NTag>
            <NText depth="3">{{ translateReason(replica.reason) }}</NText>
            <NText v-if="replica.last_restart_at" depth="3" class="mono">
              {{ t("runtimeHealth.lastRestart") }}: {{ relativeTime(replica.last_restart_at) }}
            </NText>
            <NText v-if="replica.last_oom_at" depth="3" class="mono">
              {{ t("runtimeHealth.lastOom") }}: {{ relativeTime(replica.last_oom_at) }}
            </NText>
          </li>
        </ul>
      </div>

      <NSpace :size="12">
        <NButton v-if="props.showLogs" text type="primary" @click="emit('view-logs')">
          {{ t("runtimeHealth.logs") }}
        </NButton>
        <NButton v-if="props.showDeployments" text type="primary" @click="emit('view-deployments')">
          {{ t("runtimeHealth.deployments") }}
        </NButton>
      </NSpace>

      <section v-if="props.canWrite" class="controls" :aria-label="t('runtimeHealth.monitoring')">
        <NText depth="3">{{ t("runtimeHealth.monitoringHint") }}</NText>
        <label class="control-row">
          <NSwitch
            v-model:value="enabled"
            :aria-label="t('runtimeHealth.enabled')"
            @update:value="dirty = true"
          />
          <NText>{{ t("runtimeHealth.enabled") }}</NText>
        </label>
        <label class="control-row">
          <NText>{{ t("runtimeHealth.grace") }}</NText>
          <NInputNumber
            v-model:value="grace"
            class="grace-input"
            :min="RUNTIME_MONITOR_MIN_GRACE_SECONDS"
            :max="RUNTIME_MONITOR_MAX_GRACE_SECONDS"
            :step="10"
            :status="graceError ? 'error' : undefined"
            @update:value="dirty = true"
          />
        </label>
        <NText v-if="graceError" type="error" depth="3">{{ graceError }}</NText>
        <NText v-else depth="3">{{ t("runtimeHealth.graceHint") }}</NText>
        <NButton
          type="primary"
          size="small"
          :loading="props.saving"
          :disabled="graceError !== ''"
          data-testid="runtime-health-save"
          @click="submit"
        >
          {{ t("runtimeHealth.save") }}
        </NButton>
      </section>
    </NSpace>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.grace-input {
  width: var(--field-width-narrow);
}

.summary {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: var(--space-1) var(--space-3);
  align-items: baseline;
}

.replicas ul {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: var(--space-2) 0 0;
  padding: 0;
  list-style: none;
}

.replica {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  align-items: center;
}

.controls {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
}

.control-row {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}
</style>
