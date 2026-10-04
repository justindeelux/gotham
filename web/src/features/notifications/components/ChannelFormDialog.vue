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
import { useChannelDialog } from "@/features/notifications/composables/useChannelDialog";

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

const kindOptions: Array<{ label: string; value: NotificationKind }> = [
  { label: "Discord webhook", value: "discord" },
  { label: "Slack incoming webhook", value: "slack" },
  { label: "Telegram bot", value: "telegram" },
  { label: "Email (SMTP)", value: "email" },
];
</script>

<template>
  <NModal
    v-model:show="formOpen"
    preset="card"
    :title="editing ? 'Edit notification channel' : 'New notification channel'"
    style="width: 560px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="formError" type="error" :show-icon="true">
        {{ formError }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false" class="channel-form form-container">
        <div class="form-row">
          <NFormItem label="Name">
            <NInput
              v-model:value="form.name"
              placeholder="e.g. Deploy alerts"
              aria-label="Channel name"
            />
          </NFormItem>
          <NFormItem label="Kind">
            <NSelect
              v-model:value="form.kind"
              :options="kindOptions"
              :disabled="editing"
              aria-label="Channel kind"
            />
          </NFormItem>
        </div>

        <template v-if="form.kind === 'discord' || form.kind === 'slack'">
          <NFormItem label="Webhook URL">
            <NInput
              v-model:value="form.webhook_url"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editing
                  ? 'Leave the masked value to keep the stored URL'
                  : 'https://discord.com/api/webhooks/…'
              "
              aria-label="Webhook URL"
            />
          </NFormItem>
        </template>

        <template v-else-if="form.kind === 'telegram'">
          <NFormItem label="Bot token">
            <NInput
              v-model:value="form.bot_token"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editing ? 'Leave the masked value to keep the stored token' : '123456:ABC-…'
              "
              aria-label="Bot token"
            />
          </NFormItem>
          <NFormItem label="Chat ID">
            <NInput
              v-model:value="form.chat_id"
              placeholder="-100…"
              aria-label="Chat ID"
            />
          </NFormItem>
        </template>

        <template v-else>
          <NFormItem label="SMTP host">
            <NInput
              v-model:value="form.host"
              placeholder="smtp.example.com"
              aria-label="SMTP host"
            />
          </NFormItem>
          <NFormItem label="SMTP port">
            <NInput
              v-model:value="form.port"
              type="text"
              inputmode="numeric"
              placeholder="587"
              aria-label="SMTP port"
            />
          </NFormItem>
          <NFormItem label="SMTP username">
            <NInput
              v-model:value="form.username"
              placeholder="ops@example.com"
              aria-label="SMTP username"
            />
          </NFormItem>
          <NFormItem label="SMTP password">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editing
                  ? 'Leave the masked value to keep the stored password'
                  : 'Leave blank for an open relay'
              "
              aria-label="SMTP password"
            />
          </NFormItem>
          <NFormItem label="From address">
            <NInput
              v-model:value="form.from"
              placeholder="Gotham <ops@example.com>"
              aria-label="From address"
            />
          </NFormItem>
          <NFormItem label="Recipients">
            <NInput
              v-model:value="form.to"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              placeholder="ops@example.com, oncall@example.com"
              aria-label="Recipients"
            />
          </NFormItem>
        </template>

        <NFormItem label="Events">
          <div class="field-stack">
            <NSelect
              v-model:value="form.events"
              multiple
              :options="eventOptions"
              placeholder="Select at least one event"
              aria-label="Events"
            />
            <NText v-if="form.events.length === 0" type="error" class="small">
              Select at least one event.
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
          <NFormItem label="Resource scope">
            <div class="field-stack">
              <NSelect
                :value="form.resourceType"
                :options="scopeOptions"
                aria-label="Resource scope"
                @update:value="(value: string) => selectScope(value)"
              />
              <NSelect
                v-if="form.resourceType !== ''"
                v-model:value="form.resourceId"
                :options="resourcePickerOptions"
                filterable
                :placeholder="
                  form.resourceType === 'application'
                    ? 'Select an application'
                    : 'Select a database'
                "
                aria-label="Resource"
              />
              <NText v-if="unavailableScopeHint !== ''" depth="3" class="small">
                {{ unavailableScopeHint }}
              </NText>
            </div>
          </NFormItem>

          <NFormItem label="Enabled">
            <NSwitch v-model:value="form.enabled" aria-label="Channel enabled" />
          </NFormItem>
        </div>
      </NForm>
      <NText depth="3" class="small">
        Secrets travel once, are sealed server-side and are never displayed
        again — a read only returns the mask. A team-wide channel receives
        every selected event; a scoped channel only receives events of the
        selected resource.
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="formOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="saving"
          :disabled="!canSubmit"
          @click="void handleSave()"
        >
          {{ editing ? "Save" : "Create channel" }}
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
