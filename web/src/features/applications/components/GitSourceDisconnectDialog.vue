<script setup lang="ts">
import { NAlert, NButton, NModal, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { GitSourceRow } from "@/features/applications/composables/useGitSourcesPage";

const props = defineProps<{
  show: boolean;
  row: GitSourceRow | null;
  working: boolean;
  blockedNames: string[];
  blockedError: string;
}>();
const emit = defineEmits<{
  (_event: "update:show", _value: boolean): void;
  (_event: "confirm"): void;
}>();

const { t } = useI18n();

/** displayName names the connection in the confirmation. */
function displayName(): string {
  if (props.row === null) {
    return "";
  }
  return props.row.kind === "github-app"
    ? `${props.row.title} ${props.row.subtitle}`.trim()
    : `${props.row.title} ${props.row.account}`.trim();
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
      <template v-if="props.row !== null && props.row.apps.length > 0">
        <p class="lead">
          {{
            t("applications.gitSources.disconnectApps", { count: props.row.apps.length })
          }}
        </p>
        <ul class="app-list">
          <li v-for="name in props.row.apps" :key="name" class="mono">{{ name }}</li>
        </ul>
      </template>
      <p v-else class="lead">{{ t("applications.gitSources.disconnectNoApps") }}</p>
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
          :disabled="props.row === null"
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
