<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";

import type { NotificationKind } from "@/features/notifications/api/notifications";
import { kindLabel } from "@/features/notifications/api/notifications";
import { useChannelDialog } from "@/features/notifications/composables/useChannelDialog";
import { computed } from "vue";

const {
  formOpen,
  editing,
  saving,
  formError,
  form,
  eventOptions,
  outOfScopeWarning,
  eventsHint,
  scopeOptions,
  unavailableScopeHint,
  resourcePickerOptions,
  canSubmit,
  selectScope,
  handleSave,
} = useChannelDialog();

const kindOptions = computed<Array<{ label: string; value: NotificationKind }>>(
  () =>
    (["discord", "slack", "telegram", "email"] as NotificationKind[]).map(
      (kind) => ({ label: kindLabel(kind), value: kind }),
    ),
);
</script>

<template>
  <NModal
    v-model:show="formOpen"
    preset="card"
    :title="editing ? $t('notifications.dialog.editTitle') : $t('notifications.dialog.newTitle')"
    style="width: 560px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="formError" type="error" :show-icon="true">
        {{ formError }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false" class="channel-form form-container">
        <div class="form-row">
          <NFormItem :label="$t('notifications.dialog.name')">
            <NInput
              v-model:value="form.name"
              :placeholder="$t('notifications.dialog.namePlaceholder')"
              :aria-label="$t('notifications.dialog.nameAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.kind')">
            <NSelect
              v-model:value="form.kind"
              :options="kindOptions"
              :disabled="editing"
              :aria-label="$t('notifications.dialog.kindAria')"
            />
          </NFormItem>
        </div>

        <template v-if="form.kind === 'discord' || form.kind === 'slack'">
          <NFormItem :label="$t('notifications.dialog.webhookUrl')">
            <NInput
              v-model:value="form.webhook_url"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editing
                  ? $t('notifications.dialog.webhookKeepPlaceholder')
                  : $t('notifications.dialog.webhookPlaceholder')
              "
              :aria-label="$t('notifications.dialog.webhookAria')"
            />
          </NFormItem>
        </template>

        <template v-else-if="form.kind === 'telegram'">
          <NFormItem :label="$t('notifications.dialog.botToken')">
            <NInput
              v-model:value="form.bot_token"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editing ? $t('notifications.dialog.botTokenKeepPlaceholder') : $t('notifications.dialog.botTokenPlaceholder')
              "
              :aria-label="$t('notifications.dialog.botTokenAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.chatId')">
            <NInput
              v-model:value="form.chat_id"
              :placeholder="$t('notifications.dialog.chatIdPlaceholder')"
              :aria-label="$t('notifications.dialog.chatIdAria')"
            />
          </NFormItem>
        </template>

        <template v-else>
          <NFormItem :label="$t('notifications.dialog.smtpHost')">
            <NInput
              v-model:value="form.host"
              :placeholder="$t('notifications.dialog.smtpHostPlaceholder')"
              :aria-label="$t('notifications.dialog.smtpHostAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.smtpPort')">
            <NInput
              v-model:value="form.port"
              type="text"
              inputmode="numeric"
              :placeholder="$t('notifications.dialog.smtpPortPlaceholder')"
              :aria-label="$t('notifications.dialog.smtpPortAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.smtpUsername')">
            <NInput
              v-model:value="form.username"
              :placeholder="$t('notifications.dialog.smtpUsernamePlaceholder')"
              :aria-label="$t('notifications.dialog.smtpUsernameAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.smtpPassword')">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editing
                  ? $t('notifications.dialog.smtpPasswordKeepPlaceholder')
                  : $t('notifications.dialog.smtpPasswordPlaceholder')
              "
              :aria-label="$t('notifications.dialog.smtpPasswordAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.fromAddress')">
            <NInput
              v-model:value="form.from"
              :placeholder="$t('notifications.dialog.fromAddressPlaceholder')"
              :aria-label="$t('notifications.dialog.fromAddressAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('notifications.dialog.recipients')">
            <NInput
              v-model:value="form.to"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              :placeholder="$t('notifications.dialog.recipientsPlaceholder')"
              :aria-label="$t('notifications.dialog.recipientsAria')"
            />
          </NFormItem>
        </template>

        <NFormItem :label="$t('notifications.dialog.events')">
          <div class="field-stack">
            <NSelect
              v-model:value="form.events"
              multiple
              :options="eventOptions"
              :placeholder="$t('notifications.dialog.selectEvents')"
              :aria-label="$t('notifications.dialog.eventsAria')"
            />
            <NText v-if="form.events.length === 0" type="error" class="small">
              {{ $t("notifications.dialog.noEvents") }}
            </NText>
            <NText
              v-else-if="outOfScopeWarning !== ''"
              type="warning"
              class="small"
              data-testid="events-out-of-scope"
            >
              {{ outOfScopeWarning }}
            </NText>
            <NText v-else depth="3" class="small">
              {{ eventsHint }}
            </NText>
          </div>
        </NFormItem>

        <div class="form-row">
          <NFormItem :label="$t('notifications.dialog.resourceScope')">
            <div class="field-stack">
              <NSelect
                :value="form.resourceType"
                :options="scopeOptions"
                :aria-label="$t('notifications.dialog.resourceScopeAria')"
                @update:value="(value: string) => selectScope(value)"
              />
              <NSelect
                v-if="form.resourceType !== ''"
                v-model:value="form.resourceId"
                :options="resourcePickerOptions"
                filterable
                :placeholder="
                  form.resourceType === 'application'
                    ? $t('notifications.dialog.selectApp')
                    : $t('notifications.dialog.selectDb')
                "
                :aria-label="$t('notifications.dialog.resourceAria')"
              />
              <NText v-if="unavailableScopeHint !== ''" depth="3" class="small">
                {{ unavailableScopeHint }}
              </NText>
            </div>
          </NFormItem>

          <NFormItem :label="$t('notifications.dialog.enabled')">
            <NSwitch v-model:value="form.enabled" :aria-label="$t('notifications.dialog.enabledAria')" />
          </NFormItem>
        </div>
      </NForm>
      <NText depth="3" class="small">
        {{ $t("notifications.dialog.secretsNote") }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="formOpen = false">{{ $t("notifications.dialog.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="saving"
          :disabled="!canSubmit"
          @click="void handleSave()"
        >
          {{ editing ? $t("notifications.dialog.save") : $t("notifications.dialog.create") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.field-stack {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: 100%;
}
</style>
