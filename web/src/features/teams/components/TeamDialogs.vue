<script setup lang="ts">
import { NAlert, NButton, NInput, NModal, NSelect, NSpace, NText } from "naive-ui";

import { useTeamsStore } from "@/features/teams/stores/teams";
import { isInviteEmailValid, isTeamNameValid } from "@/features/teams/schemas/teams";
import { acceptLink, registerLink } from "@/features/teams/utils/teamLinks";
import { expiryLabel } from "@/shared/utils/format";
import { useTeamsPageContext } from "@/features/teams/composables/useTeamsPage";

/** TeamDialogs renders the create/rename/invite/one-time-token modals. */
const teamsStore = useTeamsStore();
const {
  createOpen,
  createName,
  createBusy,
  createError,
  renameOpen,
  renameName,
  renameBusy,
  renameError,
  inviteOpen,
  inviteEmail,
  inviteRole,
  inviteBusy,
  inviteError,
  inviteRoleOptions,
  createdInvite,
  copied,
  handleCreate,
  handleRename,
  handleCreateInvite,
  copyAcceptLink,
  closeInviteToken,
} = useTeamsPageContext();
</script>

<template>
  <NModal
    v-if="!teamsStore.featureDisabled"
    v-model:show="createOpen"
    preset="card"
    :title="$t('teams.dialogs.newTitle')"
    style="width: 460px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ $t("teams.dialogs.newDesc") }}
      </NText>
      <NInput
        v-model:value="createName"
        :placeholder="$t('teams.dialogs.teamNamePlaceholder')"
        :aria-label="$t('teams.dialogs.teamNameAria')"
        @keyup.enter="void handleCreate()"
      />
      <NAlert v-if="createError" type="error" :show-icon="true">
        {{ createError }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="createOpen = false">{{ $t("teams.dialogs.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="createBusy"
          :disabled="!isTeamNameValid(createName)"
          @click="void handleCreate()"
        >
          {{ $t("teams.dialogs.createTeam") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    v-model:show="renameOpen"
    preset="card"
    :title="$t('teams.dialogs.renameTitle')"
    style="width: 460px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NInput
        v-model:value="renameName"
        :aria-label="$t('teams.dialogs.teamNameAria')"
        @keyup.enter="void handleRename()"
      />
      <NAlert v-if="renameError" type="error" :show-icon="true">
        {{ renameError }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="renameOpen = false">{{ $t("teams.dialogs.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="renameBusy"
          :disabled="!isTeamNameValid(renameName)"
          @click="void handleRename()"
        >
          {{ $t("teams.dialogs.save") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    v-model:show="inviteOpen"
    preset="card"
    :title="$t('teams.dialogs.inviteTitle')"
    style="width: 480px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NInput
        v-model:value="inviteEmail"
        :placeholder="$t('teams.dialogs.inviteEmailPlaceholder')"
        :aria-label="$t('teams.dialogs.inviteEmailAria')"
        @keyup.enter="void handleCreateInvite()"
      />
      <NSelect
        v-model:value="inviteRole"
        :options="inviteRoleOptions"
        :aria-label="$t('teams.dialogs.inviteRoleAria')"
      />
      <NText depth="3">
        {{ $t("teams.dialogs.inviteHint") }}
      </NText>
      <NAlert v-if="inviteError" type="error" :show-icon="true">
        {{ inviteError }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="inviteOpen = false">{{ $t("teams.dialogs.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="inviteBusy"
          :disabled="!isInviteEmailValid(inviteEmail)"
          @click="void handleCreateInvite()"
        >
          {{ $t("teams.dialogs.createInvite") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    :show="createdInvite !== null"
    preset="card"
    :title="$t('teams.dialogs.tokenTitle')"
    style="width: 560px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => { if (!value) closeInviteToken(); }"
  >
    <NSpace v-if="createdInvite" vertical :size="12">
      <NAlert type="warning" :show-icon="true">
        {{ $t("teams.dialogs.tokenWarning", { email: createdInvite.email }) }}
      </NAlert>
      <NInput
        :value="registerLink(createdInvite.token)"
        readonly
        class="mono"
        :aria-label="$t('teams.dialogs.newMemberLinkAria')"
        data-testid="invite-register-link"
      />
      <NInput
        :value="acceptLink(createdInvite.token)"
        readonly
        class="mono"
        :aria-label="$t('teams.dialogs.acceptLinkAria')"
        data-testid="invite-accept-link"
      />
      <NText depth="3" class="mono token-text" data-testid="invite-token">
        {{ $t("teams.dialogs.tokenText", { token: createdInvite.token }) }}
      </NText>
      <NText depth="3">
        {{
          $t("teams.dialogs.tokenBody", {
            email: createdInvite.email,
            role: createdInvite.role,
            expiry: expiryLabel(createdInvite.expires_at),
          })
        }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="void copyAcceptLink()">
          {{ copied ? $t("teams.dialogs.copied") : $t("teams.dialogs.copyLink") }}
        </NButton>
        <NButton type="primary" @click="closeInviteToken()">{{ $t("teams.dialogs.done") }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.token-text {
  word-break: break-all;
}
</style>
