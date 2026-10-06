<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NPopconfirm, NSpin, NTag, NText } from "naive-ui";

import { roleLabel, roleTagType } from "@/features/teams/api/teams";
import { useTeamsStore } from "@/features/teams/stores/teams";
import { relativeTime } from "@/shared/utils/format";
import { useTeamsPageContext } from "@/features/teams/composables/useTeamsPage";

/** TeamsListCard renders the "Your teams" selector card. */
const teamsStore = useTeamsStore();
const { teamActionError, deleting, openRename, handleDelete } = useTeamsPageContext();
</script>

<template>
  <NCard :title="$t('teams.list.title')">
    <template #header-extra>
      <NText depth="3">{{
        teamsStore.teams.length === 1
          ? $t("teams.list.countOne", { count: teamsStore.teams.length })
          : $t("teams.list.countOther", { count: teamsStore.teams.length })
      }}</NText>
    </template>
    <NAlert
      v-if="teamActionError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
      data-testid="team-action-error"
    >
      {{ teamActionError }}
    </NAlert>
    <NSpin :show="teamsStore.loading">
      <div
        v-for="team in teamsStore.teams"
        :key="team.id"
        class="team-row"
        :class="{ 'is-selected': team.id === teamsStore.activeTeamId }"
        :data-team="team.name"
      >
        <NButton
          quaternary
          size="small"
          class="team-row__select"
          :aria-pressed="team.id === teamsStore.activeTeamId"
          @click="teamsStore.selectTeam(team.id)"
        >
          {{ team.name }}
        </NButton>
        <NTag v-if="team.is_personal" size="small" round>{{ $t("teams.page.personal") }}</NTag>
        <NTag :type="roleTagType(team.role)" size="small" round>
          {{ roleLabel(team.role) }}
        </NTag>
        <NText depth="3" class="mono team-row__id">{{ team.id.slice(0, 8) }}</NText>
        <NText depth="3" class="team-row__age">{{ relativeTime(team.created_at) }}</NText>
        <div class="team-row__actions">
          <NButton
            size="small"
            quaternary
            :disabled="team.id !== teamsStore.activeTeamId"
            @click="openRename"
          >
            {{ $t("teams.list.rename") }}
          </NButton>
          <NPopconfirm
            v-if="team.role === 'owner'"
            :positive-button-props="{ type: 'error' }"
            @positive-click="handleDelete"
          >
            <template #trigger>
              <NButton
                size="small"
                ghost
                type="error"
                :disabled="team.is_personal || team.id !== teamsStore.activeTeamId"
                :loading="deleting && team.id === teamsStore.activeTeamId"
              >
                {{ $t("teams.list.delete") }}
              </NButton>
            </template>
            {{ $t("teams.list.deleteConfirm", { name: team.name }) }}
          </NPopconfirm>
        </div>
      </div>
      <NEmpty
        v-if="
          !teamsStore.loading &&
          teamsStore.teams.length === 0 &&
          !teamsStore.error &&
          teamsStore.loaded
        "
        :description="$t('teams.list.empty')"
      />
    </NSpin>
  </NCard>
</template>

<style scoped>
.team-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid transparent;
}

.team-row.is-selected {
  background: var(--selected-row);
  border-color: var(--border);
}

.team-row__select {
  font-weight: 600;
}

.team-row__id {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}

.team-row__age {
  font-size: var(--text-xs);
}

.team-row__actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

@media (max-width: 860px) {
  .team-row {
    flex-wrap: wrap;
  }
}
</style>
