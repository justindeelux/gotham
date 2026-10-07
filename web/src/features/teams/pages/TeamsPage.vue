<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSpace,
  NTabPane,
  NTabs,
  NTag,
} from "naive-ui";
import { provide } from "vue";

import { roleLabel, roleTagType } from "@/features/teams/api/teams";
import TeamDialogs from "@/features/teams/components/TeamDialogs.vue";
import TeamInvitesPanel from "@/features/teams/components/TeamInvitesPanel.vue";
import TeamMembersPanel from "@/features/teams/components/TeamMembersPanel.vue";
import TeamsListCard from "@/features/teams/components/TeamsListCard.vue";
import { teamsPageKey, useTeamsPage } from "@/features/teams/composables/useTeamsPage";
import { useTeamsStore } from "@/features/teams/stores/teams";

/**
 * FE-8.1 (2) — teams, members and invites.
 *
 * Ported from docs/design/team-settings.html (members + invites panels) onto
 * the established settings page pattern. Every mutation is gated on the
 * caller's role as `GET /v1/teams` reports it, and the backend's protections
 * (last owner, personal team, non-empty team) surface as inline errors
 * instead of being pre-empted: the service is the authority.
 *
 * Thin route component: state and mutations live in `useTeamsPage` (provided
 * to the panels and dialogs below), sections render through them.
 */
const teamsStore = useTeamsStore();
const page = useTeamsPage();
provide(teamsPageKey, page);
const {
  createOpen,
  selectedTeam,
  isPersonal,
  members,
  invites,
  membersLoading,
  invitesLoading,
  loadTeam,
} = page;
</script>

<template>
  <div class="teams-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ $t("teams.page.eyebrow") }}</p>
        <h1>{{ $t("teams.page.title") }}</h1>
        <p class="page-desc">
          {{ $t("teams.page.descriptionPre") }}
          <span class="mono">owner</span> /
          <span class="mono">admin</span> /
          <span class="mono">read-only</span>.
          {{ $t("teams.page.descriptionPost") }}
        </p>
      </div>
      <div class="page-actions">
        <NButton
          v-if="!teamsStore.featureDisabled"
          type="primary"
          @click="createOpen = true"
        >
          {{ $t("teams.page.newTeam") }}
        </NButton>
      </div>
    </div>

    <NCard v-if="teamsStore.featureDisabled" :title="$t('teams.page.unavailableTitle')">
      <NEmpty
        :description="$t('teams.page.unavailableDesc')"
      />
    </NCard>

    <template v-else>
      <NAlert v-if="teamsStore.error" type="error" :show-icon="true">
        {{ teamsStore.error }}
      </NAlert>

      <TeamsListCard />

      <NCard v-if="selectedTeam" :title="selectedTeam.name" class="team-card">
        <template #header-extra>
          <NSpace align="center" :size="8">
            <NButton
              size="small"
              :loading="membersLoading || invitesLoading"
              @click="void loadTeam()"
            >
              {{ $t("teams.page.refresh") }}
            </NButton>
            <NTag v-if="isPersonal" size="small" round>{{ $t("teams.page.personal") }}</NTag>
            <NTag :type="roleTagType(selectedTeam.role)" size="small" round>
              {{ $t("teams.page.yourRole", { role: roleLabel(selectedTeam.role) }) }}
            </NTag>
          </NSpace>
        </template>

        <NTabs type="line" animated>
          <NTabPane name="members" :tab="$t('teams.page.membersTab', { count: members.length })">
            <TeamMembersPanel />
          </NTabPane>

          <NTabPane name="invites" :tab="$t('teams.page.invitesTab', { count: invites.length })">
            <TeamInvitesPanel />
          </NTabPane>
        </NTabs>
      </NCard>
    </template>

    <TeamDialogs />
  </div>
</template>

<style scoped>
.teams-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

/**
 * Selected-team header: the title plus Refresh/role tags share one Naive
 * card header row. At narrow widths the title takes a full row and the
 * extra block drops below it, so the full team name stays readable in both
 * languages (adaptation: the shipped card evolved past the team-settings
 * mockup tabs, which have no card header). A 100% title basis forces the
 * break — a 100% extra basis would sum to exactly one line and starve the
 * title instead. No font-size change; wrapping only.
 */
.team-card :deep(.n-card-header) {
  flex-wrap: wrap;
  row-gap: var(--space-2);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
  }

  .team-card :deep(.n-card-header__main) {
    flex: 1 1 100%;
  }
}
</style>
