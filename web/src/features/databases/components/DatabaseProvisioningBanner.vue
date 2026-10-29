<script setup lang="ts">
import { NAlert } from "naive-ui";
import { computed } from "vue";

import type { DatabaseStatus } from "@/features/databases/api/databases";
import { i18n } from "@/shared/i18n";

interface Props {
  status?: DatabaseStatus;
  statusMessage?: string;
}

function t(key: string): string {
  return String(i18n.global.t(key));
}

const props = defineProps<Props>();

const creating = computed<boolean>(() => props.status === "creating");
const errorMessage = computed<string>(() =>
  props.status === "error" ? (props.statusMessage ?? "") : "",
);
</script>

<template>
  <NAlert v-if="creating" type="info" :show-icon="true">
    {{ t("databases.detail.creating.banner") }}
  </NAlert>
  <NAlert
    v-else-if="errorMessage"
    type="error"
    :show-icon="true"
    :title="t('databases.detail.creating.failedTitle')"
  >
    {{ errorMessage }}
  </NAlert>
</template>
