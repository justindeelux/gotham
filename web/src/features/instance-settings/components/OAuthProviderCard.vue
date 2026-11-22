<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSwitch,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveOAuthProvider } from "@/features/instance-settings/api/oauthProviders";
import type { OAuthProviderInfo } from "@/features/instance-settings/api/oauthProviders";
import {
  oauthClientIdSchema,
  oauthClientSecretSchema,
} from "@/features/instance-settings/schemas/oauthProviders";
import { useCopyText } from "@/shared/composables/useCopyText";
import { useSectionForm } from "@/shared/composables/useSectionForm";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

interface Props {
  provider: OAuthProviderInfo;
  callbackUrl: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{ saved: [info: OAuthProviderInfo] }>();
const { t } = useI18n();
const { copyText } = useCopyText();

const formRef = ref<FormInst | null>(null);
const form = reactive({ clientId: "", clientSecret: "", enabled: false });

watch(
  () => props.provider,
  (value) => {
    form.clientId = value.client_id;
    form.clientSecret = "";
    form.enabled = value.enabled;
  },
  { immediate: true },
);

const rules: FormRules = {
  clientId: ruleFrom(oauthClientIdSchema, { when: () => form.enabled }),
  clientSecret: ruleFrom(oauthClientSecretSchema, {
    when: () => form.enabled && !props.provider.has_secret,
  }),
};

const secretPlaceholder = computed<string>(() =>
  props.provider.has_secret
    ? t("instance-settings.oauth.clientSecretSaved")
    : t("instance-settings.oauth.clientSecretPlaceholder"),
);

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveOAuthProvider(props.provider.name, {
      client_id: form.clientId.trim(),
      ...(form.clientSecret.trim() === ""
        ? {}
        : { client_secret: form.clientSecret.trim() }),
      enabled: form.enabled,
    }),
  (next) => emit("saved", next),
  () => t("instance-settings.oauth.saved"),
);

async function handleSubmit(): Promise<void> {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  await submit();
}

function status(path: string): "error" | undefined {
  return serverErrors.value[path] ? "error" : undefined;
}
</script>

<template>
  <NCard>
    <template #header>
      <div class="oauth-card-header">
        <span>{{ t(`instance-settings.oauth.providers.${provider.name}`) }}</span>
        <span class="oauth-toggle">
          <NSwitch
            v-model:value="form.enabled"
            :aria-label="t('instance-settings.oauth.enabled')"
          />
          <span>{{ t("instance-settings.oauth.enabled") }}</span>
        </span>
      </div>
    </template>

    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="oauth-alert">
      {{ errorMessage }}
    </NAlert>
    <NForm
      ref="formRef"
      :model="form"
      :rules="rules"
      class="oauth-form"
      @submit.prevent="handleSubmit"
    >
      <NFormItem
        path="clientId"
        :label="t('instance-settings.oauth.clientId')"
        :validation-status="status('client_id')"
        :feedback="serverErrors.client_id"
      >
        <NInput
          v-model:value="form.clientId"
          :input-props="{ id: `oauth-${provider.name}-client-id` }"
        />
      </NFormItem>

      <NFormItem
        path="clientSecret"
        :label="t('instance-settings.oauth.clientSecret')"
        :validation-status="status('client_secret')"
        :feedback="serverErrors.client_secret"
      >
        <NInput
          v-model:value="form.clientSecret"
          type="password"
          show-password-on="click"
          :placeholder="secretPlaceholder"
          :input-props="{ id: `oauth-${provider.name}-client-secret` }"
        />
      </NFormItem>

      <NFormItem :label="t('instance-settings.oauth.callback')">
        <NInput
          :value="callbackUrl"
          readonly
          :input-props="{ id: `oauth-${provider.name}-callback` }"
        >
          <template #suffix>
            <NButton
              text
              size="small"
              :aria-label="t('instance-settings.oauth.copyCallback')"
              @click="copyText(callbackUrl, t('instance-settings.oauth.callback'))"
            >
              {{ t("instance-settings.oauth.copy") }}
            </NButton>
          </template>
        </NInput>
      </NFormItem>

      <p class="oauth-hint">
        {{ t(`instance-settings.oauth.setup.${provider.name}`) }}
      </p>

      <NButton type="primary" :loading="submitting" @click="handleSubmit">
        {{ t("instance-settings.oauth.save") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.oauth-form {
  max-width: 640px;
}

.oauth-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.oauth-toggle {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 400;
  color: var(--fg-2);
}

.oauth-alert {
  margin-bottom: var(--space-4);
}

.oauth-hint {
  margin: var(--space-2) 0 var(--space-4);
  font-size: var(--text-xs);
  color: var(--muted);
  line-height: 1.6;
}
</style>
