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
import { computed } from "vue";
import { useI18n } from "vue-i18n";

export type DomainProtocol = "http" | "https";

interface Props {
  show: boolean;
  title: string;
  submitLabel: string;
  host: string;
  protocol: DomainProtocol;
  protocolOptions: Array<{ label: string; value: DomainProtocol }>;
  saving: boolean;
  error: string | null;
  hint: string;
  hostError?: string;
  /** null hides the enabled switch (features without a disable state). */
  enabled?: boolean | null;
}

const props = withDefaults(defineProps<Props>(), { enabled: null, hostError: "" });

const emit = defineEmits<{
  "update:show": [value: boolean];
  "update:host": [value: string];
  "update:protocol": [value: DomainProtocol];
  "update:enabled": [value: boolean];
  save: [];
  cancel: [];
}>();

const { t } = useI18n();

const showEnabled = computed<boolean>(() => props.enabled !== null);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="props.title"
    class="app-modal domain-form-modal"
    style="width: 520px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="props.error" type="error" :show-icon="true">
        {{ props.error }}
      </NAlert>
      <slot name="warning" />
      <NForm label-placement="top" :show-feedback="false">
        <NFormItem :label="t('domains.form.host')">
          <NInput
            :value="props.host"
            class="mono domain-host"
            placeholder="app.example.com"
            :input-props="{ 'aria-label': t('domains.form.hostAria') }"
            @update:value="(value: string) => emit('update:host', value)"
          />
        </NFormItem>
        <div v-if="props.hostError" class="field-error">{{ props.hostError }}</div>
        <div class="domain-routing-row">
          <slot name="routing" />
          <NFormItem :label="t('domains.form.protocol')" class="domain-routing-field">
            <NSelect
              :value="props.protocol"
              class="domain-protocol"
              :options="props.protocolOptions"
              :aria-label="t('domains.form.protocolAria')"
              @update:value="(value: DomainProtocol) => emit('update:protocol', value)"
            />
          </NFormItem>
        </div>
        <slot name="extra" />
        <NFormItem v-if="showEnabled" :label="t('domains.form.enabled')">
          <NSwitch
            :value="props.enabled === true"
            :aria-label="t('domains.form.enabledAria')"
            @update:value="(value: boolean) => emit('update:enabled', value)"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" class="small">{{ props.hint }}</NText>
      <slot name="hint" />
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="emit('cancel')">
          {{ t("domains.form.cancel") }}
        </NButton>
        <NButton
          type="primary"
          class="domain-submit"
          :loading="props.saving"
          @click="emit('save')"
        >
          {{ props.submitLabel }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.field-error {
  margin-top: calc(-1 * var(--space-2));
  margin-bottom: var(--space-3);
  font-size: var(--text-xs);
  color: var(--danger);
  overflow-wrap: anywhere;
}

.domain-protocol {
  width: 100%;
}

.domain-routing-row {
  display: flex;
  gap: var(--space-3);
}

.domain-routing-field {
  flex: 1;
  min-width: 0;
}
</style>
