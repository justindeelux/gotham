<script setup lang="ts">
import { NAlert, NButton, NInput, NModal, NSelect, NSpace, NText } from "naive-ui";

import { useTeamsStore } from "@/features/teams/stores/teams";
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
    title="New team"
    style="width: 460px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        You become the new team's owner. Resources can be moved in from the
        applications and servers you already manage.
      </NText>
      <NInput
        v-model:value="createName"
        placeholder="Team name"
        aria-label="Team name"
        @keyup.enter="void handleCreate()"
      />
      <NAlert v-if="createError" type="error" :show-icon="true">
        {{ createError }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="createOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="createBusy"
          :disabled="createName.trim() === ''"
          @click="void handleCreate()"
        >
          Create team
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    v-model:show="renameOpen"
    preset="card"
    title="Rename team"
    style="width: 460px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NInput
        v-model:value="renameName"
        aria-label="Team name"
        @keyup.enter="void handleRename()"
      />
      <NAlert v-if="renameError" type="error" :show-icon="true">
        {{ renameError }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="renameOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="renameBusy"
          :disabled="renameName.trim() === ''"
          @click="void handleRename()"
        >
          Save
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    v-model:show="inviteOpen"
    preset="card"
    title="Invite member"
    style="width: 480px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NInput
        v-model:value="inviteEmail"
        placeholder="name@example.com"
        aria-label="Invite email"
        @keyup.enter="void handleCreateInvite()"
      />
      <NSelect
        v-model:value="inviteRole"
        :options="inviteRoleOptions"
        aria-label="Invite role"
      />
      <NText depth="3">
        Invites can grant admin or read-only — ownership is transferred from
        the members table, never invited.
      </NText>
      <NAlert v-if="inviteError" type="error" :show-icon="true">
        {{ inviteError }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="inviteOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="inviteBusy"
          :disabled="inviteEmail.trim() === ''"
          @click="void handleCreateInvite()"
        >
          Create invite
        </NButton>
      </NSpace>
    </template>
  </NModal>

  <NModal
    :show="createdInvite !== null"
    preset="card"
    title="Invite created — copy the link now"
    style="width: 560px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => { if (!value) closeInviteToken(); }"
  >
    <NSpace v-if="createdInvite" vertical :size="12">
      <NAlert type="warning" :show-icon="true">
        This link is shown once and never stored. Email delivery is not wired
        yet (BE-8.3) — send it to {{ createdInvite.email }} yourself.
      </NAlert>
      <NInput
        :value="registerLink(createdInvite.token)"
        readonly
        class="mono"
        aria-label="New member link"
        data-testid="invite-register-link"
      />
      <NInput
        :value="acceptLink(createdInvite.token)"
        readonly
        class="mono"
        aria-label="Accept link"
        data-testid="invite-accept-link"
      />
      <NText depth="3" class="mono token-text" data-testid="invite-token">
        token: {{ createdInvite.token }}
      </NText>
      <NText depth="3">
        Send the first link to {{ createdInvite.email }} to create a new
        account: registration is closed on an instance that already has an
        account, so the invite is the only way in. The second link is for
        someone who already has an account and is signed in. Either way the
        member joins as <span class="mono">{{ createdInvite.role }}</span
        >. It expires {{ expiryLabel(createdInvite.expires_at) }}.
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="void copyAcceptLink()">
          {{ copied ? "Copied" : "Copy link" }}
        </NButton>
        <NButton type="primary" @click="closeInviteToken()">Done</NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.token-text {
  word-break: break-all;
}
</style>
