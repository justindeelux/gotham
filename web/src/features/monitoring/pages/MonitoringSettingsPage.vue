<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NFormItem,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type {
  MonitoringRules,
  MonitoringSettings,
} from "@/features/monitoring/api/monitoring";
import {
  describeMonitoringError,
  getMonitoringSettings,
  resetMonitoringSettings,
  saveMonitoringSettings,
} from "@/features/monitoring/api/monitoring";
import FilesystemRuleFields from "@/features/monitoring/components/FilesystemRuleFields.vue";
import HostRuleFields from "@/features/monitoring/components/HostRuleFields.vue";
import { monitoringRulesSchema } from "@/features/monitoring/schemas/monitoring";
import { listServers } from "@/features/servers/api/servers";
import type { Server } from "@/features/servers/api/servers";

const { t, locale } = useI18n();
const message = useMessage();

const servers = ref<Server[]>([]);
const scope = ref("instance");
const settings = ref<MonitoringSettings | null>(null);
const rules = ref<MonitoringRules | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
let loadToken = 0;

const options = computed(() => [
  { label: t("monitoring.scope.instance"), value: "instance" },
  ...servers.value.map((server) => ({ label: server.name, value: server.id })),
]);

const sourceLabel = computed(() => {
  switch (settings.value?.source) {
    case "node":
      return t("monitoring.source.node");
    case "instance":
      return t("monitoring.source.instance");
    default:
      return t("monitoring.source.default");
  }
});

const validationError = computed(() => {
  if (!rules.value) {
    return "";
  }
  const result = monitoringRulesSchema.safeParse(rules.value);
  if (result.success) {
    return "";
  }
  const issue = result.error.issues[0];
  return t(issue?.message ?? "monitoring.validation.range");
});

async function load(): Promise<void> {
  const token = ++loadToken;
  const selected = scope.value;
  loading.value = true;
  error.value = "";
  rules.value = null;
  settings.value = null;
  try {
    const next = await getMonitoringSettings(
      selected === "instance" ? undefined : selected,
    );
    if (token !== loadToken) {
      return;
    }
    settings.value = next;
    rules.value = JSON.parse(JSON.stringify(next.effective)) as MonitoringRules;
  } catch (err) {
    if (token !== loadToken) {
      return;
    }
    error.value = describeMonitoringError(err);
  } finally {
    if (token === loadToken) {
      loading.value = false;
    }
  }
}

async function save(): Promise<void> {
  if (!rules.value || validationError.value) {
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    await saveMonitoringSettings(
      rules.value,
      scope.value === "instance" ? undefined : scope.value,
    );
    message.success(t("monitoring.actions.saved"));
    await load();
  } catch (err) {
    error.value = describeMonitoringError(err);
  } finally {
    saving.value = false;
  }
}

async function reset(): Promise<void> {
  saving.value = true;
  error.value = "";
  try {
    await resetMonitoringSettings(
      scope.value === "instance" ? undefined : scope.value,
    );
    message.success(t("monitoring.actions.resetDone"));
    await load();
  } catch (err) {
    error.value = describeMonitoringError(err);
  } finally {
    saving.value = false;
  }
}

watch(scope, () => {
  void load();
});

onMounted(async () => {
  try {
    servers.value = await listServers();
  } catch {
    servers.value = [];
  }
  await load();
});
</script>

<template>
  <div class="monitoring-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("monitoring.page.eyebrow") }}</p>
        <h1>{{ t("monitoring.page.title") }}</h1>
        <p class="page-desc">{{ t("monitoring.page.description") }}</p>
      </div>
    </div>

    <NAlert v-if="error" type="error" :show-icon="true" class="page-alert">
      {{ error }}
    </NAlert>

    <NSpace vertical :size="16" class="monitoring-panels">
      <NCard>
        <NSpace align="center" :size="12" wrap>
          <NSelect
            v-model:value="scope"
            :options="options"
            :aria-label="t('monitoring.scope.label')"
            class="scope-select"
          />
          <NTag :bordered="false">{{ sourceLabel }}</NTag>
          <span class="muted">{{ t("monitoring.scope.hint") }}</span>
        </NSpace>
      </NCard>

      <NSpin :show="loading">
        <NSpace v-if="rules" vertical :size="16">
          <NCard :title="t('monitoring.rules.cpuTitle')">
            <p class="rule-hint">{{ t("monitoring.rules.cpuHint") }}</p>
            <HostRuleFields v-model:rule="rules.cpu" />
          </NCard>
          <NCard :title="t('monitoring.rules.memoryTitle')">
            <p class="rule-hint">{{ t("monitoring.rules.memoryHint") }}</p>
            <HostRuleFields v-model:rule="rules.memory" />
          </NCard>
          <NCard :title="t('monitoring.rules.spaceTitle')">
            <p class="rule-hint">{{ t("monitoring.rules.spaceHint") }}</p>
            <FilesystemRuleFields v-model:rule="rules.space" />
            <NFormItem :label="t('monitoring.rules.forecast')">
              <NSwitch
                v-model:value="rules.forecast"
                :aria-label="t('monitoring.rules.forecast')"
              />
            </NFormItem>
            <p class="rule-hint">{{ t("monitoring.rules.forecastHint") }}</p>
          </NCard>
          <NCard :title="t('monitoring.rules.inodeTitle')">
            <p class="rule-hint">{{ t("monitoring.rules.inodeHint") }}</p>
            <FilesystemRuleFields v-model:rule="rules.inode" />
          </NCard>

          <NAlert v-if="validationError" type="warning" :show-icon="true">
            {{ validationError }}
          </NAlert>

          <p class="muted" :lang="locale">
            {{ t("monitoring.rules.defaultsNote") }}
          </p>
        </NSpace>

        <NSpace class="monitoring-actions">
          <NButton
            type="primary"
            :loading="saving"
            :disabled="Boolean(validationError) || loading || !rules"
            @click="save"
          >
            {{ t("monitoring.actions.save") }}
          </NButton>
          <NButton
            :loading="saving"
            :disabled="loading || !rules"
            @click="reset"
          >
            {{
              scope === "instance"
                ? t("monitoring.actions.reset")
                : t("monitoring.actions.resetNode")
            }}
          </NButton>
        </NSpace>
      </NSpin>
    </NSpace>
  </div>
</template>

<style scoped>
.monitoring-panels {
  max-width: 980px;
}

.scope-select {
  min-width: 220px;
}

.monitoring-actions {
  margin-top: var(--space-4);
}

.rule-hint {
  margin: 0 0 var(--space-3);
  color: var(--muted);
  font-size: var(--text-xs);
}

.page-alert {
  margin-bottom: var(--space-4);
}

.muted {
  color: var(--muted);
  font-size: var(--text-xs);
}
</style>
