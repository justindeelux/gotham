<script setup lang="ts">
import { NCard, NSelect, NSpace, NTag, NText } from "naive-ui";

import { roleLabel, roleTagType } from "@/features/teams";
import { useTeamsStore } from "@/features/teams";
import { useChannelDialog } from "@/features/notifications/composables/useChannelDialog";

const teamsStore = useTeamsStore();
const { teamOptions, activeRole } = useChannelDialog();
</script>

<template>
  <NCard title="Team scope">
    <NSpace align="center" :size="12" wrap>
      <NSelect
        :value="teamsStore.activeTeamId"
        :options="teamOptions"
        filterable
        style="width: 260px"
        aria-label="Notification team"
        @update:value="(value: string) => teamsStore.selectTeam(value)"
      />
      <NTag
        v-if="activeRole"
        :type="roleTagType(activeRole)"
        size="small"
        round
      >
        your role: {{ roleLabel(activeRole) }}
      </NTag>
      <NText depth="3">
        Channels below belong to this team. A read-only role can look but
        not change anything.
      </NText>
    </NSpace>
  </NCard>
</template>
