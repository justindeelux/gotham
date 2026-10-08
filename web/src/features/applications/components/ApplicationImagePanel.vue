<script setup lang="ts">
import { NAlert, NButton, NCard, NFormItem, NInput, NSpace, useMessage } from "naive-ui";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeApplicationError,
  updateApplication,
  type Application,
  type UpdateApplicationInput,
} from "@/features/applications/api/applications";
import { imageRefSchema } from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const { t } = useI18n();
const message = useMessage();
const appsStore = useApplicationsStore();

/**
 * Image-source editor: the prebuilt reference and the optional
 * private-registry credential. Blank credential fields are omitted, so saving
 * a new reference never clears a stored credential by accident; the clear
 * button wipes it explicitly. The credential is write-only: the API only
 * reports whether one is stored.
 */
const imageRef = ref<string>(props.application.image_ref);
const username = ref<string>("");
const password = ref<string>("");
const saving = ref<boolean>(false);
const error = ref<string | null>(null);

watch(
  () => props.application.image_ref,
  (value) => {
    imageRef.value = value;
  },
);

/** handleSave stores the edited reference and any typed credential half. */
async function handleSave(): Promise<void> {
  const nextRef = imageRef.value.trim();
  if (!imageRefSchema.safeParse(nextRef).success) {
    error.value = t("applications.image.refInvalid");
    return;
  }
  error.value = null;
  saving.value = true;
  try {
    const input: UpdateApplicationInput = {};
    if (nextRef !== props.application.image_ref) {
      input.image_ref = nextRef;
    }
    if (username.value.trim() !== "") {
      input.registry_username = username.value.trim();
    }
    if (password.value !== "") {
      input.registry_password = password.value;
    }
    if (Object.keys(input).length === 0) {
      return;
    }
    await updateApplication(props.application.id, input);
    username.value = "";
    password.value = "";
    await appsStore.fetchApplication(props.application.id);
    message.success(t("applications.image.saved"));
  } catch (submitError) {
    error.value = describeApplicationError(submitError);
  } finally {
    saving.value = false;
  }
}

/** handleClear wipes the stored credential explicitly. */
async function handleClear(): Promise<void> {
  error.value = null;
  saving.value = true;
  try {
    await updateApplication(props.application.id, {
      registry_username: "",
      registry_password: "",
    });
    username.value = "";
    password.value = "";
    await appsStore.fetchApplication(props.application.id);
    message.success(t("applications.image.cleared"));
  } catch (submitError) {
    error.value = describeApplicationError(submitError);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <NCard :title="t('applications.image.title')" class="image-panel">
    <NSpace vertical :size="12">
      <NFormItem :label="t('applications.image.ref')">
        <NInput v-model:value="imageRef" class="mono" />
        <span class="field-hint">{{ t("applications.image.refHint") }}</span>
      </NFormItem>

      <NFormItem :label="t('applications.image.credential')">
        <span class="field-hint field-hint--block">{{
          props.application.has_registry_credential
            ? t("applications.image.configured")
            : t("applications.image.unconfigured")
        }}</span>
        <NInput
          v-model:value="username"
          class="mono"
          :placeholder="t('applications.image.usernamePlaceholder')"
          :aria-label="t('applications.image.username')"
        />
        <NInput
          v-model:value="password"
          type="password"
          show-password-on="click"
          class="field-gap"
          :placeholder="t('applications.image.passwordPlaceholder')"
          :aria-label="t('applications.image.password')"
        />
        <span class="field-hint">{{ t("applications.image.credentialHint") }}</span>
      </NFormItem>

      <NAlert v-if="error" type="error" :show-icon="true">
        {{ error }}
      </NAlert>

      <NSpace :size="8">
        <NButton
          type="primary"
          :loading="saving"
          @click="void handleSave()"
        >
          {{ t("applications.image.save") }}
        </NButton>
        <NButton
          v-if="props.application.has_registry_credential"
          :disabled="saving"
          @click="void handleClear()"
        >
          {{ t("applications.image.clear") }}
        </NButton>
      </NSpace>
    </NSpace>
  </NCard>
</template>

<style scoped>
.image-panel {
  margin-top: 16px;
}

.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.field-hint--block {
  margin-bottom: 8px;
}

.field-gap {
  margin-top: 8px;
}

.mono {
  font-family: var(--font-mono);
}
</style>
