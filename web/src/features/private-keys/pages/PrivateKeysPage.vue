<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NFormItem,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h } from "vue";
import { useI18n } from "vue-i18n";

import { usePrivateKeysPage } from "@/features/private-keys/composables/usePrivateKeysPage";
import type { PrivateKey } from "@/features/private-keys/schemas/keys";
import { compactPublicKey } from "@/features/private-keys/utils/randomName";
import { useCopyText } from "@/shared/composables/useCopyText";
import { formatDate } from "@/shared/utils/format";

const { t } = useI18n();
const page = usePrivateKeysPage();
const { copyText } = useCopyText();

function usedByText(key: PrivateKey | null): string {
  if (!key) {
    return "";
  }
  const count = page.usageById.value[key.id] ?? 0;
  return String(
    t(
      count === 1
        ? "private-keys.list.usedByOne"
        : "private-keys.list.usedByOther",
      { count },
    ),
  );
}

function copyRevealedPrivateKey(): void {
  const revealed = page.showing.value;
  if (!revealed) {
    return;
  }
  void copyText(revealed.privateKey, t("private-keys.reveal.aria"));
}

const columns: DataTableColumns<PrivateKey> = [
  {
    title: String(t("private-keys.list.columns.name")),
    key: "name",
    width: 180,
    render: (key) => h("strong", {}, key.name),
  },
  {
    title: String(t("private-keys.list.columns.type")),
    key: "type",
    width: 100,
    render: (key) =>
      key.type
        ? h(
            NTag,
            { size: "small", bordered: false },
            { default: () => key.type },
          )
        : t("private-keys.list.emptyValue"),
  },
  {
    title: String(t("private-keys.list.columns.description")),
    key: "description",
    width: 220,
    render: (key) =>
      h(
        "span",
        { class: "description-cell" },
        key.description || t("private-keys.list.emptyValue"),
      ),
  },
  {
    title: String(t("private-keys.list.columns.publicKey")),
    key: "public_key",
    width: 240,
    render: (key) =>
      key.public_key
        ? h(
            "code",
            { class: "public-key", title: key.public_key },
            compactPublicKey(key.public_key),
          )
        : t("private-keys.list.emptyValue"),
  },
  {
    title: String(t("private-keys.list.columns.created")),
    key: "created_at",
    width: 130,
    render: (key) => formatDate(key.created_at),
  },
  {
    title: String(t("private-keys.list.columns.usedBy")),
    key: "used_by",
    width: 110,
    render: (key) => usedByText(key),
  },
  {
    title: String(t("private-keys.list.columns.actions")),
    key: "actions",
    width: 420,
    render: (key) =>
      h(NSpace, { size: 4, wrap: false }, {
        default: () => [
          h(
            NButton,
            {
              size: "small",
              quaternary: true,
              "data-testid": "private-key-view",
              onClick: () => page.openView(key),
            },
            { default: () => t("private-keys.actions.view") },
          ),
          h(
            NButton,
            {
              size: "small",
              quaternary: true,
              "data-testid": "private-key-copy-public",
              "aria-label": t("private-keys.actions.copyPublicKeyAria", {
                name: key.name,
              }),
              disabled: !key.public_key,
              onClick: () =>
                void copyText(
                  key.public_key,
                  t("private-keys.list.columns.publicKey"),
                ),
            },
            { default: () => t("private-keys.actions.copy") },
          ),
          h(
            NPopconfirm,
            {
              positiveText: t("private-keys.confirm.revealPositive"),
              negativeText: t("common.actions.cancel"),
              onPositiveClick: () => void page.reveal(key),
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: "small",
                    quaternary: true,
                    "data-testid": "private-key-reveal",
                    loading: page.revealingId.value === key.id,
                  },
                  { default: () => t("private-keys.actions.reveal") },
                ),
              default: () =>
                t("private-keys.confirm.reveal", { name: key.name }),
            },
          ),
          h(
            NButton,
            {
              size: "small",
              quaternary: true,
              "data-testid": "private-key-edit",
              onClick: () => page.openEdit(key),
            },
            { default: () => t("private-keys.actions.edit") },
          ),
          h(
            NPopconfirm,
            {
              positiveText: t("private-keys.confirm.deletePositive"),
              negativeText: t("common.actions.cancel"),
              onPositiveClick: () => void page.remove(key),
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: "small",
                    quaternary: true,
                    type: "error",
                    "data-testid": "private-key-delete",
                    loading: page.removingId.value === key.id,
                  },
                  { default: () => t("private-keys.actions.delete") },
                ),
              default: () =>
                t("private-keys.confirm.delete", { name: key.name }),
            },
          ),
        ],
      }),
  },
];
</script>

<template>
  <div class="private-keys-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("private-keys.page.eyebrow") }}</p>
        <h1>{{ t("private-keys.page.title") }}</h1>
        <p class="page-desc">{{ t("private-keys.page.description") }}</p>
      </div>
      <div class="page-actions">
        <NButton
          type="primary"
          data-testid="private-keys-create"
          @click="page.openCreate"
        >
          {{ t("private-keys.page.create") }}
        </NButton>
      </div>
    </div>

    <NAlert v-if="page.errorMessage.value" type="error" :show-icon="true">
      {{ page.errorMessage.value }}
    </NAlert>
    <div v-if="page.errorMessage.value" class="retry-row">
      <NButton size="small" @click="page.refresh">
        {{ t("private-keys.list.retry") }}
      </NButton>
    </div>

    <NCard :title="t('private-keys.list.title')">
      <NSpin :show="page.loading.value">
        <NEmpty
          v-if="page.loaded.value && page.keys.value.length === 0"
          :description="t('private-keys.list.empty')"
        />
        <NDataTable
          v-else
          data-testid="private-keys-table"
          :columns="columns"
          :data="page.keys.value"
          :row-key="(key: PrivateKey) => key.id"
          :bordered="false"
          :scroll-x="1400"
        />
      </NSpin>
    </NCard>

    <NModal
      :show="page.createOpen.value"
      preset="card"
      class="app-modal"
      style="width: 600px; max-width: 94vw"
      :title="t('private-keys.create.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeCreate(); }"
    >
      <NSpace vertical :size="14">
        <NAlert v-if="page.createErrorMessage.value" type="error" :show-icon="true">
          {{ page.createErrorMessage.value }}
        </NAlert>

        <NFormItem
          :label="t('private-keys.create.name')"
          :label-props="{ for: 'private-key-name' }"
          :validation-status="page.createNameError.value ? 'error' : undefined"
        >
          <NSpace vertical :size="4" class="field-stack">
            <NInput
              v-model:value="page.createName.value"
              :placeholder="t('private-keys.create.namePlaceholder')"
              maxlength="64"
              :input-props="{
                id: 'private-key-name',
                autocomplete: 'off',
                'aria-describedby': page.createNameError.value
                  ? 'private-key-name-error'
                  : undefined,
                'aria-invalid': page.createNameError.value ? 'true' : undefined,
              }"
              @input="page.touchCreate"
            />
            <span class="field-hint">{{ t("private-keys.create.nameHint") }}</span>
            <span
              v-if="page.createNameError.value"
              id="private-key-name-error"
              class="field-error"
              role="alert"
            >
              {{ page.createNameError.value }}
            </span>
          </NSpace>
        </NFormItem>

        <NFormItem
          :label="t('private-keys.create.description')"
          :label-props="{ for: 'private-key-description' }"
        >
          <NInput
            v-model:value="page.createDescription.value"
            :placeholder="t('private-keys.create.descriptionPlaceholder')"
            maxlength="500"
            :input-props="{ id: 'private-key-description' }"
            @input="page.touchCreate"
          />
        </NFormItem>

        <NTabs v-model:value="page.createTab.value" type="line" animated>
          <NTabPane name="generate" :tab="t('private-keys.create.tabs.generate')">
            <NFormItem
              :label="t('private-keys.create.type')"
              :label-props="{ for: 'private-key-type' }"
            >
              <NSpace vertical :size="4" class="field-stack">
                <NSelect
                  v-model:value="page.createType.value"
                  :options="page.typeOptions.value"
                  :input-props="{ id: 'private-key-type' }"
                />
                <span class="field-hint">{{ t("private-keys.create.typeHint") }}</span>
              </NSpace>
            </NFormItem>
          </NTabPane>

          <NTabPane name="import" :tab="t('private-keys.create.tabs.import')">
            <NFormItem
              :label="t('private-keys.create.privateKey')"
              :label-props="{ for: 'private-key-pem' }"
              :validation-status="page.importPrivateKeyError.value ? 'error' : undefined"
            >
              <NSpace vertical :size="4" class="field-stack">
                <NInput
                  v-model:value="page.importPrivateKeyValue.value"
                  type="textarea"
                  :rows="6"
                  :placeholder="t('private-keys.create.privateKeyPlaceholder')"
                  class="mono"
                  :input-props="{
                    id: 'private-key-pem',
                    autocomplete: 'off',
                    spellcheck: 'false',
                    'aria-describedby': page.importPrivateKeyError.value
                      ? 'private-key-pem-error'
                      : undefined,
                    'aria-invalid': page.importPrivateKeyError.value
                      ? 'true'
                      : undefined,
                  }"
                  @input="page.touchCreate"
                />
                <span class="field-hint">{{ t("private-keys.create.privateKeyHint") }}</span>
                <span
                  v-if="page.importPrivateKeyError.value"
                  id="private-key-pem-error"
                  class="field-error"
                  role="alert"
                >
                  {{ page.importPrivateKeyError.value }}
                </span>
              </NSpace>
            </NFormItem>

            <NFormItem
              :label="t('private-keys.create.publicKey')"
              :label-props="{ for: 'private-key-public-key' }"
            >
              <NSpace vertical :size="4" class="field-stack">
                <NInput
                  v-model:value="page.importPublicKeyValue.value"
                  type="textarea"
                  :rows="3"
                  :placeholder="t('private-keys.create.publicKeyPlaceholder')"
                  class="mono"
                  :input-props="{
                    id: 'private-key-public-key',
                    autocomplete: 'off',
                    spellcheck: 'false',
                  }"
                />
                <span class="field-hint">{{ t("private-keys.create.publicKeyHint") }}</span>
              </NSpace>
            </NFormItem>
          </NTabPane>
        </NTabs>
      </NSpace>

      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="page.closeCreate">
            {{ t("private-keys.create.cancel") }}
          </NButton>
          <NButton
            type="primary"
            data-testid="private-keys-submit"
            :loading="page.createBusy.value"
            @click="page.submitCreate"
          >
            {{ t("private-keys.create.submit") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="page.viewing.value !== null"
      preset="card"
      class="app-modal"
      style="width: 640px; max-width: 94vw"
      :title="t('private-keys.view.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeView(); }"
    >
      <NDescriptions
        v-if="page.viewing.value"
        :column="1"
        bordered
        label-placement="left"
      >
        <NDescriptionsItem :label="t('private-keys.view.name')">
          {{ page.viewing.value.name }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('private-keys.view.type')">
          {{ page.viewing.value.type || t("private-keys.list.emptyValue") }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('private-keys.view.description')">
          {{ page.viewing.value.description || t("private-keys.list.emptyValue") }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('private-keys.view.publicKey')">
          <code class="detail-value">{{
            page.viewing.value.public_key || t("private-keys.list.emptyValue")
          }}</code>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('private-keys.view.created')">
          {{ formatDate(page.viewing.value.created_at) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('private-keys.view.usedBy')">
          {{ usedByText(page.viewing.value) }}
        </NDescriptionsItem>
      </NDescriptions>

      <template #footer>
        <NSpace justify="end">
          <NButton @click="page.closeView">
            {{ t("private-keys.view.close") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="page.editing.value !== null"
      preset="card"
      class="app-modal"
      style="width: 560px; max-width: 94vw"
      :title="t('private-keys.edit.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeEdit(); }"
    >
      <NSpace vertical :size="14">
        <NAlert v-if="page.editErrorMessage.value" type="error" :show-icon="true">
          {{ page.editErrorMessage.value }}
        </NAlert>
        <NFormItem
          :label="t('private-keys.create.name')"
          :label-props="{ for: 'private-key-edit-name' }"
          :validation-status="page.editNameError.value ? 'error' : undefined"
        >
          <NInput
            v-model:value="page.editName.value"
            maxlength="64"
            :input-props="{
              id: 'private-key-edit-name',
              autocomplete: 'off',
              'aria-describedby': page.editNameError.value
                ? 'private-key-edit-name-error'
                : undefined,
              'aria-invalid': page.editNameError.value ? 'true' : undefined,
            }"
            @input="page.touchEdit"
          />
          <span
            v-if="page.editNameError.value"
            id="private-key-edit-name-error"
            class="field-error"
            role="alert"
          >
            {{ page.editNameError.value }}
          </span>
        </NFormItem>
        <NFormItem
          :label="t('private-keys.create.description')"
          :label-props="{ for: 'private-key-edit-description' }"
        >
          <NInput
            v-model:value="page.editDescription.value"
            maxlength="500"
            :placeholder="t('private-keys.create.descriptionPlaceholder')"
            :input-props="{ id: 'private-key-edit-description' }"
            @input="page.touchEdit"
          />
        </NFormItem>
      </NSpace>

      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="page.closeEdit">
            {{ t("private-keys.edit.cancel") }}
          </NButton>
          <NButton
            type="primary"
            data-testid="private-keys-edit-save"
            :loading="page.editBusy.value"
            @click="page.submitEdit"
          >
            {{ t("private-keys.edit.submit") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="page.showing.value !== null"
      preset="card"
      class="app-modal"
      style="width: 680px; max-width: 94vw"
      :title="t('private-keys.reveal.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeReveal(); }"
    >
      <NSpace v-if="page.showing.value" vertical :size="14">
        <NAlert type="warning" :show-icon="true">
          {{ t("private-keys.reveal.warning") }}
        </NAlert>
        <NInput
          :value="page.showing.value.privateKey"
          type="textarea"
          :rows="9"
          readonly
          class="mono"
          :input-props="{
            id: 'private-key-secret',
            'aria-label': t('private-keys.reveal.aria'),
            autocomplete: 'off',
            spellcheck: 'false',
          }"
        />
      </NSpace>

      <template #footer>
        <NSpace v-if="page.showing.value" justify="end" :size="8">
          <NButton
            data-testid="private-keys-reveal-copy"
            @click="copyRevealedPrivateKey"
          >
            {{ t("private-keys.reveal.copy") }}
          </NButton>
          <NButton
            type="primary"
            data-testid="private-keys-reveal-done"
            @click="page.closeReveal"
          >
            {{ t("private-keys.reveal.done") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.private-keys-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.retry-row {
  margin-top: calc(var(--space-2) * -1);
}

.description-cell {
  color: var(--fg-2);
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.public-key,
.detail-value {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.public-key {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-value {
  overflow-wrap: anywhere;
}

.mono :deep(textarea) {
  font-family: var(--font-mono);
}

.field-stack {
  width: 100%;
}

.field-hint {
  color: var(--muted);
  font-size: var(--text-xs);
}

.field-error {
  color: var(--danger);
  font-size: var(--text-xs);
}
</style>
