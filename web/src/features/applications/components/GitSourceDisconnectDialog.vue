<script setup lang="ts">
import { NAlert, NButton, NCheckbox, NModal, NSpace } from "naive-ui";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { GitSourceRow } from "@/features/applications/composables/useGitSourcesPage";

const props = defineProps<{
  show: boolean;
  row: GitSourceRow | null;
  working: boolean;
  /** False while the usage lookup is pending or failed: the list below is
   * not trustworthy, so disconnecting needs an explicit acknowledgement. */
  usageKnown: boolean;
  blockedNames: string[];
  blockedError: string;
}>();
const emit = defineEmits<{
  (_event: "update:show", _value: boolean): void;
  (_event: "confirm"): void;
}>();

const { t } = useI18n();
/** acknowledged lets an unknown-usage disconnect through explicitly. */
const acknowledged = ref(false);

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      acknowledged.value = false;
    }
  },
);

/** displayName names the connection in the confirmation. */
function displayName(): string {
  if (props.row === null) {
    return "";
  }
  return props.row.kind === "github-app"
    ? `${props.row.title} ${props.row.subtitle}`.trim()
    : `${props.row.title} ${props.row.account}`.trim();
}

/** confirmBlocked disables the button until unknown usage is acknowledged. */
function confirmBlocked(): boolean {
  return props.row === null || (!props.usageKnown && !acknowledged.value);
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('applications.gitSources.disconnectTitle')"
    class="dialog-card"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpace vertical :size="16">
      <p class="lead">{{ t("applications.gitSources.disconnectLead", { name: displayName() }) }}</p>
      <NAlert
        v-if="props.row !== null && props.row.kind === 'github-app'"
        type="warning"
        :show-icon="true"
      >
        {{ t("applications.gitSources.disconnectGithubNote") }}
      </NAlert>
      <template v-if="props.usageKnown && props.row !== null && props.row.apps.length > 0">
        <p class="lead">
          {{
            t("applications.gitSources.disconnectApps", { count: props.row.apps.length })
          }}
        </p>
        <ul class="app-list">
          <li v-for="name in props.row.apps" :key="name" class="mono">{{ name }}</li>
        </ul>
      </template>
      <p v-else-if="props.usageKnown" class="lead">{{ t("applications.gitSources.disconnectNoApps") }}</p>
      <template v-else>
        <NAlert type="warning" :show-icon="true">
          {{ t("applications.gitSources.disconnectUnknown") }}
        </NAlert>
        <NCheckbox v-model:checked="acknowledged">
          {{ t("applications.gitSources.disconnectAcknowledge") }}
        </NCheckbox>
      </template>
      <NAlert v-if="props.blockedError" type="error" :show-icon="true">
        {{
          props.blockedNames.length > 0
            ? t("applications.gitSources.disconnectBlocked", { names: props.blockedNames.join(", ") })
            : props.blockedError
        }}
      </NAlert>
      <NSpace :size="12">
        <NButton
          type="error"
          :loading="props.working"
          :disabled="confirmBlocked()"
          @click="emit('confirm')"
        >
          {{ t("applications.gitSources.disconnectConfirm") }}
        </NButton>
        <NButton :disabled="props.working" @click="emit('update:show', false)">
          {{ t("applications.gitSources.cancel") }}
        </NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>

<style scoped>
.dialog-card {
  max-width: 520px;
}

.lead {
  margin: 0;
}

.app-list {
  margin: 0;
  padding-left: 20px;
  display: grid;
  gap: 4px;
}

.mono {
  font-family: var(--font-mono);
}
</style>
