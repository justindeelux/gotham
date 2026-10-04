<script setup lang="ts">
import { NAvatar, NButton, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { RouterLink } from "vue-router";

import type { Server } from "@/features/servers/api/servers";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import {
  detailInitials,
  summaryLine,
} from "@/features/servers/utils/serverDetailView";

interface Props {
  server: Server;
  validating: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  edit: [];
  validate: [];
}>();

/** initials derives a two-letter avatar from the server name. */
const initials = computed<string>(() => detailInitials(props.server.name));

/** summary renders the one-line node summary under the title. */
const summary = computed<string>(() => summaryLine(props.server));
</script>

<template>
  <div class="page-head">
    <NAvatar round :size="48">{{ initials }}</NAvatar>
    <div class="page-head__title">
      <NSpace align="center" :size="10">
        <NText strong style="font-size: 20px">{{ server.name }}</NText>
        <ServerStatusTag :status="server.status" size="medium" />
      </NSpace>
      <NText depth="3">{{ summary }}</NText>
    </div>
    <NSpace class="page-head__actions" align="center" :size="8">
      <NButton @click="emit('edit')">
        Edit
      </NButton>
      <NButton :loading="validating" @click="emit('validate')">
        Validate
      </NButton>
      <RouterLink
        :to="{ name: 'server-containers', params: { id: server.id } }"
        custom
      >
        <template #default="{ navigate }">
          <NButton type="primary" @click="navigate">
            Open containers
          </NButton>
        </template>
      </RouterLink>
    </NSpace>
  </div>
</template>

<style scoped>
.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}
</style>
