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
  NRadio,
  NRadioGroup,
  NSpace,
  NSpin,
  NTag,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import { useApiTokensPage } from "@/features/api-tokens/composables/useApiTokensPage";
import type { ApiToken } from "@/features/api-tokens/schemas/tokens";
import { useCopyText } from "@/shared/composables/useCopyText";
import { formatDate, relativeTime } from "@/shared/utils/format";

const { t } = useI18n();
const authStore = useAuthStore();
const page = useApiTokensPage();
const { copyText } = useCopyText();

const columns: DataTableColumns<ApiToken> = [
  {
    title: String(t("api-tokens.list.columns.name")),
    key: "name",
    render: (token) => h("strong", {}, token.name),
  },
  {
    title: String(t("api-tokens.list.columns.scopes")),
    key: "scopes",
    width: 160,
    render: (token) =>
      h(
        NSpace,
        { size: 4, wrap: false },
        {
          default: () =>
            token.scopes.map((scope) =>
              h(
                NTag,
                { size: "small", bordered: false, key: scope },
                { default: () => t(`api-tokens.scopes.${scope}.name`) },
              ),
            ),
        },
      ),
  },
  {
    title: String(t("api-tokens.list.columns.created")),
    key: "created_at",
    width: 130,
    render: (token) => formatDate(token.created_at),
  },
  {
    title: String(t("api-tokens.list.columns.lastUsed")),
    key: "last_used_at",
    width: 130,
    render: (token) =>
      token.last_used_at
        ? relativeTime(token.last_used_at, null, { long: true })
        : t("api-tokens.list.neverUsed"),
  },
  {
    title: String(t("api-tokens.list.columns.status")),
    key: "status",
    width: 150,
    render: (token) =>
      token.revoked_at
        ? h("div", { class: "status-cell" }, [
            h(
              NTag,
              { size: "small", type: "error", bordered: false },
              { default: () => t("api-tokens.list.revoked") },
            ),
            h(
              "span",
              { class: "muted small" },
              t("api-tokens.list.revokedOn", {
                date: formatDate(token.revoked_at),
              }),
            ),
          ])
        : h(
            NTag,
            { size: "small", type: "success", bordered: false },
            { default: () => t("api-tokens.list.active") },
          ),
  },
  {
    title: String(t("api-tokens.list.columns.actions")),
    key: "actions",
    width: 190,
    render: (token) => {
      if (token.revoked_at) {
        return h("span", { class: "muted small" }, "—");
      }
      return h(NSpace, { size: 4, wrap: false }, {
        default: () => [
          h(
            NPopconfirm,
            {
              positiveText: t("api-tokens.list.rotatePositive"),
              negativeText: t("common.actions.cancel"),
              onPositiveClick: () => void page.rotate(token),
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: "small",
                    quaternary: true,
                    loading: page.rotatingId.value === token.id,
                  },
                  { default: () => t("api-tokens.list.rotate") },
                ),
              default: () =>
                t("api-tokens.list.rotateConfirm", { name: token.name }),
            },
          ),
          h(
            NPopconfirm,
            {
              positiveText: t("api-tokens.list.revokePositive"),
              negativeText: t("common.actions.cancel"),
              onPositiveClick: () => void page.revoke(token),
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: "small",
                    quaternary: true,
                    type: "error",
                    loading: page.revokingId.value === token.id,
                  },
                  { default: () => t("api-tokens.list.revoke") },
                ),
              default: () =>
                t("api-tokens.list.revokeConfirm", { name: token.name }),
            },
          ),
        ],
      });
    },
  },
];

onMounted(() => {
  if (authStore.isAuthenticated) {
    void authStore.fetchMe().catch(() => {});
  }
});
</script>

<template>
  <div class="api-tokens-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("api-tokens.page.eyebrow") }}</p>
        <h1>{{ t("api-tokens.page.title") }}</h1>
        <p class="page-desc">{{ t("api-tokens.page.description") }}</p>
      </div>
      <div class="page-actions">
        <NButton
          type="primary"
          data-testid="api-tokens-create"
          @click="page.openCreate"
        >
          {{ t("api-tokens.page.create") }}
        </NButton>
      </div>
    </div>

    <NAlert v-if="page.errorMessage.value" type="error" :show-icon="true">
      {{ page.errorMessage.value }}
    </NAlert>
    <div v-if="page.errorMessage.value" class="retry-row">
      <NButton size="small" @click="page.refresh">
        {{ t("api-tokens.list.retry") }}
      </NButton>
    </div>

    <NCard :title="t('api-tokens.list.title')">
      <NSpin :show="page.loading.value">
        <NEmpty
          v-if="page.loaded.value && page.tokens.value.length === 0"
          :description="t('api-tokens.list.empty')"
        />
        <NDataTable
          v-else
          data-testid="api-tokens-table"
          :columns="columns"
          :data="page.tokens.value"
          :row-key="(token: ApiToken) => token.id"
          :row-class-name="(token: ApiToken) => (token.revoked_at ? 'is-revoked' : '')"
          :bordered="false"
          :scroll-x="940"
        />
      </NSpin>
    </NCard>

    <NModal
      :show="page.createOpen.value"
      preset="card"
      class="app-modal"
      style="width: 560px; max-width: 94vw"
      :title="t('api-tokens.create.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeCreate(); }"
    >
      <NSpace vertical :size="14">
        <NAlert v-if="page.createErrorMessage.value" type="error" :show-icon="true">
          {{ page.createErrorMessage.value }}
        </NAlert>
        <NFormItem
          :label="t('api-tokens.create.name')"
          :feedback="page.createNameError.value ?? ''"
          :validation-status="page.createNameError.value ? 'error' : undefined"
        >
          <NSpace vertical :size="4" class="field-stack">
            <NInput
              v-model:value="page.createName.value"
              :placeholder="t('api-tokens.create.namePlaceholder')"
              maxlength="64"
              :input-props="{
                id: 'api-tokens-name',
                autocomplete: 'off',
              }"
              @input="page.touchCreate"
              @keyup.enter="page.submitCreate"
            />
            <span class="field-hint">{{ t("api-tokens.create.nameHint") }}</span>
          </NSpace>
        </NFormItem>

        <fieldset class="scope-field">
          <legend class="scope-legend">{{ t("api-tokens.create.scope") }}</legend>
          <NRadioGroup v-model:value="page.createScope.value">
            <NSpace vertical :size="10">
              <NRadio
                v-for="scope in page.scopeOptions.value"
                :key="scope"
                :value="scope"
              >
                <span class="scope-option">
                  <strong>{{ t(`api-tokens.scopes.${scope}.name`) }}</strong>
                  <span class="scope-description">
                    {{ t(`api-tokens.scopes.${scope}.description`) }}
                  </span>
                </span>
              </NRadio>
            </NSpace>
          </NRadioGroup>
        </fieldset>

        <NSpace justify="end">
          <NButton @click="page.closeCreate">
            {{ t("common.actions.cancel") }}
          </NButton>
          <NButton
            type="primary"
            :loading="page.createBusy.value"
            @click="page.submitCreate"
          >
            {{ t("api-tokens.create.submit") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>

    <NModal
      :show="page.revealed.value !== null"
      preset="card"
      class="app-modal"
      style="width: 560px; max-width: 94vw"
      :title="t('api-tokens.reveal.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeReveal(); }"
    >
      <NSpace v-if="page.revealed.value" vertical :size="14">
        <NAlert type="warning" :show-icon="true">
          {{ t("api-tokens.reveal.warning") }}
        </NAlert>
        <NInput
          :value="page.revealed.value.token"
          readonly
          class="mono"
          :input-props="{
            id: 'api-tokens-secret',
            'aria-label': t('api-tokens.reveal.aria'),
          }"
        />
        <NSpace justify="end">
          <NButton
            @click="copyText(page.revealed.value.token, t('api-tokens.reveal.copyLabel'))"
          >
            {{ t("api-tokens.reveal.copy") }}
          </NButton>
          <NButton type="primary" data-testid="api-tokens-reveal-done" @click="page.closeReveal">
            {{ t("api-tokens.reveal.done") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>
  </div>
</template>

<style scoped>
.api-tokens-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.retry-row {
  margin-top: calc(var(--space-2) * -1);
}

.token-name {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.status-cell {
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

.field-stack {
  width: 100%;
}

.scope-field {
  margin: 0;
  padding: 0;
  border: 0;
  min-width: 0;
}

.scope-legend {
  margin-bottom: var(--space-2);
  padding: 0;
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.scope-option {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.scope-description {
  color: var(--muted);
  font-size: var(--text-xs);
}

:deep(.is-revoked td) {
  color: var(--muted);
}

@media (max-width: 640px) {
  .page-actions {
    margin-left: 0;
  }
}
</style>
