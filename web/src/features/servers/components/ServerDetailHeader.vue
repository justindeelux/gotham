<script setup lang="ts">
import { NAvatar, NButton } from "naive-ui";
import { computed } from "vue";
import { RouterLink } from "vue-router";

import type { Server } from "@/features/servers/api/servers";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import {
  detailInitials,
  summaryLine,
} from "@/features/servers/utils/serverDetailView";
import PageHeader from "@/shared/ui/PageHeader.vue";

interface Props {
  server: Server;
  validating: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  edit: [];
  validate: [];
}>();

const initials = computed<string>(() => detailInitials(props.server.name));

const summary = computed<string>(() => summaryLine(props.server));
</script>

<template>
  <PageHeader>
    <template #identity>
      <NAvatar round :size="48">{{ initials }}</NAvatar>
    </template>
    <template #title>{{ server.name }}</template>
    <template #badges>
      <ServerStatusTag :status="server.status" size="medium" />
    </template>
    <template #meta>{{ summary }}</template>
    <template #actions>
      <NButton @click="emit('edit')">
        {{ $t("servers.card.edit") }}
      </NButton>
      <NButton :loading="validating" @click="emit('validate')">
        {{ $t("servers.detail.validateAction") }}
      </NButton>
      <RouterLink
        :to="{ name: 'server-containers', params: { id: server.id } }"
        custom
      >
        <template #default="{ navigate }">
          <NButton type="primary" @click="navigate">
            {{ $t("servers.detail.openContainers") }}
          </NButton>
        </template>
      </RouterLink>
    </template>
  </PageHeader>
</template>
