<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NFormItem,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTag,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h, computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useUsersPage } from "@/features/users/composables/useUsersPage";
import type { Account, Invite } from "@/features/users/schemas/users";
import { isInviteEmailValid } from "@/features/users/schemas/users";
import { formatDate } from "@/shared/utils/format";

const { t } = useI18n();
const page = useUsersPage();
const emailTouched = ref(false);

const emailInvalid = computed<boolean>(
  () =>
    emailTouched.value &&
    !isInviteEmailValid(page.inviteEmail.value),
);

const userColumns: DataTableColumns<Account> = [
  {
    title: String(t("users.columns.user")),
    key: "email",
    render: (account) =>
      h("div", { class: "user-cell" }, [
        h("strong", {}, account.display_name?.trim() || account.email),
        h("span", { class: "muted small" }, account.email),
      ]),
  },
  {
    title: String(t("users.columns.role")),
    key: "role",
    width: 120,
    render: (account) =>
      h(
        NTag,
        {
          size: "small",
          type: account.role === "owner" ? "success" : "default",
          bordered: false,
        },
        { default: () => t(`common.roles.${account.role}`) },
      ),
  },
  {
    title: String(t("users.columns.projects")),
    key: "projects",
    render: (account) =>
      account.role === "owner"
        ? t("users.allProjects")
        : account.projects.length === 0
          ? t("users.noProjects")
          : account.projects.map((project) => project.name).join(", "),
  },
  {
    title: String(t("users.columns.joined")),
    key: "created_at",
    width: 140,
    render: (account) => formatDate(account.created_at),
  },
  {
    title: String(t("users.columns.actions")),
    key: "actions",
    width: 240,
    render: (account) =>
      account.role === "owner"
        ? h("span", { class: "muted small" }, t("users.ownerHint"))
        : h(NSpace, { size: 4, wrap: false }, {
            default: () => [
              h(
                NButton,
                {
                  size: "small",
                  quaternary: true,
                  onClick: () => page.openAccess(account),
                },
                { default: () => t("users.actions.projects") },
              ),
              h(
                NPopconfirm,
                {
                  onPositiveClick: () => void page.reset(account),
                },
                {
                  trigger: () =>
                    h(
                      NButton,
                      { size: "small", quaternary: true },
                      { default: () => t("users.actions.reset") },
                    ),
                  default: () => t("users.resetConfirm", { email: account.email }),
                },
              ),
              h(
                NPopconfirm,
                {
                  onPositiveClick: () => void page.remove(account),
                },
                {
                  trigger: () =>
                    h(
                      NButton,
                      { size: "small", quaternary: true, type: "error" },
                      { default: () => t("users.actions.remove") },
                    ),
                  default: () => t("users.removeConfirm", { email: account.email }),
                },
              ),
            ],
          }),
  },
];

const inviteColumns: DataTableColumns<Invite> = [
  { title: String(t("users.columns.email")), key: "email" },
  {
    title: String(t("users.columns.projects")),
    key: "projects",
    render: (invite) =>
      invite.projects.length === 0
        ? t("users.noProjects")
        : invite.projects.map((project) => project.name).join(", "),
  },
  {
    title: String(t("users.columns.expires")),
    key: "expires_at",
    width: 140,
    render: (invite) => formatDate(invite.expires_at),
  },
  {
    title: "",
    key: "actions",
    width: 120,
    render: (invite) =>
      h(
        NPopconfirm,
        { onPositiveClick: () => void page.revoke(invite) },
        {
          trigger: () =>
            h(
              NButton,
              { size: "small", quaternary: true, type: "error" },
              { default: () => t("users.actions.revoke") },
            ),
          default: () => t("users.revokeConfirm", { email: invite.email }),
        },
      ),
  },
];

function closeInvite(): void {
  emailTouched.value = false;
  page.closeInvite();
}

function submitInvite(): void {
  emailTouched.value = true;
  void page.submitInvite();
}
</script>

<template>
  <div class="users-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ $t("users.page.eyebrow") }}</p>
        <h1>{{ $t("users.page.title") }}</h1>
        <p class="page-desc">{{ $t("users.page.description") }}</p>
      </div>
      <div class="page-actions">
        <NButton type="primary" @click="page.openInvite">
          {{ $t("users.page.invite") }}
        </NButton>
      </div>
    </div>

    <NAlert v-if="page.error.value" type="error" :show-icon="true">
      {{ page.error.value }}
    </NAlert>

    <NSpin :show="page.loading.value">
      <NCard :title="$t('users.members.title')">
        <NDataTable
          data-testid="users-table"
          :columns="userColumns"
          :data="page.users.value"
          :row-key="(account: Account) => account.id"
          :bordered="false"
          :scroll-x="720"
        />
      </NCard>

      <NCard :title="$t('users.invites.title')" class="invites-card">
        <NDataTable
          v-if="page.invites.value.length > 0"
          :columns="inviteColumns"
          :data="page.invites.value"
          :row-key="(invite: Invite) => invite.id"
          :bordered="false"
          :scroll-x="640"
        />
        <NEmpty v-else :description="$t('users.invites.empty')" />
      </NCard>
    </NSpin>

    <NModal
      :show="page.inviteOpen.value"
      preset="card"
      class="app-modal"
      style="width: 520px; max-width: 94vw"
      :title="$t('users.invite.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) closeInvite(); }"
    >
      <NSpace vertical :size="14">
        <NAlert v-if="page.inviteError.value" type="error" :show-icon="true">
          {{ page.inviteError.value }}
        </NAlert>
        <NAlert v-if="page.inviteLinkValue.value" type="success" :show-icon="true">
          <p>{{ $t("users.invite.linkHint") }}</p>
          <NInput
            :value="page.inviteLinkValue.value"
            readonly
            class="mono"
            :input-props="{ 'aria-label': $t('users.invite.linkAria') }"
          />
        </NAlert>
        <template v-if="!page.inviteLinkValue.value">
          <NFormItem
            :label="$t('users.invite.email')"
            :feedback="emailInvalid ? $t('users.validation.emailInvalid') : ''"
            :validation-status="emailInvalid ? 'error' : undefined"
          >
            <NInput
              :value="page.inviteEmail.value"
              placeholder="person@example.com"
              :input-props="{ 'aria-label': $t('users.invite.email') }"
              @update:value="(value: string) => { page.inviteEmail.value = value; }"
            />
          </NFormItem>
          <NFormItem :label="$t('users.invite.projects')">
            <NSelect
              v-model:value="page.inviteProjects.value"
              multiple
              filterable
              :options="page.projects.value"
              :placeholder="$t('users.invite.projectsPlaceholder')"
            />
          </NFormItem>
        </template>
        <NSpace justify="end">
          <NButton @click="closeInvite">{{ $t("common.actions.close") }}</NButton>
          <NButton
            v-if="page.inviteLinkValue.value"
            type="primary"
            @click="page.copyInviteLink"
          >
            {{ $t("users.invite.copyLink") }}
          </NButton>
          <NButton
            v-else
            type="primary"
            :loading="page.inviteBusy.value"
            :disabled="!page.inviteEmail.value.trim()"
            @click="submitInvite"
          >
            {{ $t("users.invite.submit") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>

    <NModal
      :show="page.accessTarget.value !== null"
      preset="card"
      class="app-modal"
      style="width: 520px; max-width: 94vw"
      :title="$t('users.access.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeAccess(); }"
    >
      <NSpace vertical :size="14">
        <p class="muted">{{ page.accessTarget.value?.email }}</p>
        <NAlert v-if="page.accessError.value" type="error" :show-icon="true">
          {{ page.accessError.value }}
        </NAlert>
        <NFormItem :label="$t('users.access.projects')">
          <NSelect
            v-model:value="page.accessProjects.value"
            multiple
            filterable
            :options="page.projects.value"
            :placeholder="$t('users.invite.projectsPlaceholder')"
          />
        </NFormItem>
        <NSpace justify="end">
          <NButton @click="page.closeAccess()">{{ $t("common.actions.cancel") }}</NButton>
          <NButton
            type="primary"
            :loading="page.accessBusy.value"
            @click="page.submitAccess"
          >
            {{ $t("common.actions.save") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>
  </div>
</template>

<style scoped>
.users-page {
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

.page-desc,
.muted {
  color: var(--muted);
}

.page-desc {
  margin: 0;
}

.page-actions {
  margin-left: auto;
}

.invites-card {
  margin-top: var(--space-4);
}

:deep(.user-cell) {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.small {
  font-size: var(--text-xs);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 640px) {
  .page-actions {
    margin-left: 0;
  }
}
</style>
