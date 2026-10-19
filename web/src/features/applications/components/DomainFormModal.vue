<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NText,
} from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { DomainProtocol } from "@/features/applications/api/applications";

interface Props {
  show: boolean;
  editing: boolean;
  domain: string;
  port: number | null;
  service: string;
  protocol: DomainProtocol;
  saving: boolean;
  error: string | null;
  isCompose: boolean;
  serviceOptions: Array<{ label: string; value: string }>;
  protocolOptions: Array<{ label: string; value: DomainProtocol }>;
  /** Old host with a certificate intent that the rename will delete. */
  renameCertificateHost: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  "update:domain": [value: string];
  "update:port": [value: number | null];
  "update:protocol": [value: DomainProtocol];
  "service-change": [value: string | null];
  save: [];
  cancel: [];
}>();

const { t } = useI18n();

const title = computed(() =>
  props.editing
    ? t("applications.domain.editTitle")
    : t("applications.domain.addTitle"),
);

const submitLabel = computed(() =>
  props.editing
    ? t("applications.domain.save")
    : t("applications.domain.add"),
);

/** showRenameWarning marks a hostname change that drops an existing intent. */
const showRenameWarning = computed<boolean>(
  () =>
    props.editing &&
    props.renameCertificateHost !== "" &&
    props.domain.trim().toLowerCase() !== props.renameCertificateHost,
);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="title"
    class="app-modal domain-form-modal"
    style="width: 520px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="props.error" type="error" :show-icon="true">
        {{ props.error }}
      </NAlert>
      <NAlert v-if="showRenameWarning" type="warning" :show-icon="true">
        {{ t("applications.domain.renameCertWarning", { domain: props.renameCertificateHost }) }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false">
        <NFormItem :label="t('applications.domain.host')">
          <NInput
            :value="props.domain"
            class="mono domain-host"
            placeholder="app.example.com"
            :input-props="{ 'aria-label': t('applications.domain.hostAria') }"
            @update:value="(value: string) => emit('update:domain', value)"
          />
        </NFormItem>
        <NFormItem :label="t('applications.domain.port')">
          <NInputNumber
            :value="props.port"
            class="domain-port"
            :min="1"
            :max="65535"
            placeholder="3000"
            :input-props="{ 'aria-label': t('applications.domain.portAria') }"
            @update:value="(value: number | null) => emit('update:port', value)"
          />
        </NFormItem>
        <NFormItem
          v-if="props.isCompose"
          :label="t('applications.domain.service')"
        >
          <NSelect
            :value="props.service"
            class="domain-service"
            filterable
            tag
            clearable
            :options="props.serviceOptions"
            :placeholder="t('applications.domain.servicePlaceholder')"
            :aria-label="t('applications.domain.serviceAria')"
            @update:value="(value: string | null) => emit('service-change', value)"
          />
        </NFormItem>
        <NFormItem :label="t('applications.domain.protocol')">
          <NSelect
            :value="props.protocol"
            class="domain-protocol"
            :options="props.protocolOptions"
            :aria-label="t('applications.domain.protocolAria')"
            @update:value="(value: DomainProtocol) => emit('update:protocol', value)"
          />
        </NFormItem>
      </NForm>
      <NText v-if="props.isCompose" depth="3" class="small">
        {{ t("applications.domain.serviceHint") }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="emit('cancel')">
          {{ t("applications.domain.cancel") }}
        </NButton>
        <NButton
          type="primary"
          class="domain-submit"
          :loading="props.saving"
          @click="emit('save')"
        >
          {{ submitLabel }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.domain-port {
  width: 100%;
  max-width: 180px;
}

.domain-service,
.domain-protocol {
  width: 100%;
  max-width: 320px;
}
</style>
