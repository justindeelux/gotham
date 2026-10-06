<script setup lang="ts">
import { NCard, NSelect, NSpace, NTag, NText } from "naive-ui";

import { roleLabel, roleTagType } from "@/features/teams";
import { useTeamsStore } from "@/features/teams";
import { useChannelDialog } from "@/features/notifications/composables/useChannelDialog";

const teamsStore = useTeamsStore();
const { teamOptions, activeRole } = useChannelDialog();
</script>

<template>
  <NCard :title="$t('notifications.scope.title')">
    <NSpace align="center" :size="12" wrap>
      <NSelect
        :value="teamsStore.activeTeamId"
        :options="teamOptions"
        filterable
        style="width: 260px"
        :aria-label="$t('notifications.scope.teamAria')"
        @update:value="(value: string) => teamsStore.selectTeam(value)"
      />
      <NTag
        v-if="activeRole"
        :type="roleTagType(activeRole)"
        size="small"
        round
      >
        {{ $t("notifications.scope.yourRole", { role: roleLabel(activeRole) }) }}
      </NTag>
      <NText depth="3">
        {{ $t("notifications.scope.readOnlyNote") }}
      </NText>
    </NSpace>
  </NCard>
</template>
