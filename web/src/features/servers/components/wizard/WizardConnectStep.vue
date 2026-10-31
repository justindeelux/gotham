<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NRadioButton,
  NRadioGroup,
  NSelect,
} from "naive-ui";
import type { FormInst } from "naive-ui";
import { computed, inject, onUnmounted, ref, watch } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";

const wizard = inject(WizardKey);
if (!wizard) {
  throw new Error("WizardConnectStep must be used inside AddServerWizard.");
}

const formRef = ref<FormInst | null>(null);
watch(formRef, (instance) => {
  wizard.setFormRef(instance);
});
onUnmounted(() => {
  wizard.setFormRef(null);
});

const keyOptions = computed(() =>
  wizard.privateKeys.value.map((key) => ({
    label: key.type ? `${key.name} (${key.type})` : key.name,
    value: key.id,
  })),
);
</script>

<template>
  <NForm
    ref="formRef"
    :model="wizard.form"
    :rules="wizard.rules.value"
    label-placement="top"
    @submit.prevent="wizard.handleCreate"
  >
    <div class="connect-form form-container">
      <NAlert v-if="wizard.hasCreatedServer.value" type="info" :show-icon="true">
        {{ $t("servers.wizard.alreadyRegistered") }}
      </NAlert>

      <section class="connect-group" :aria-label="$t('servers.wizard.connectIdentity')">
        <h4 class="connect-group__title">{{ $t("servers.wizard.connectIdentity") }}</h4>
        <div class="form-row">
          <NFormItem
            :label="$t('servers.wizard.nodeName')"
            path="name"
            :label-props="{ for: 'add-server-name' }"
          >
            <NInput
              v-model:value="wizard.form.name"
              placeholder="build-node-03"
              :input-props="{ id: 'add-server-name', 'aria-label': $t('servers.wizard.nodeName') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.nodeNameHint") }}</span>
          </NFormItem>
          <NFormItem
            :label="$t('servers.wizard.sshUser')"
            path="sshUser"
            :label-props="{ for: 'add-server-ssh-user' }"
          >
            <NInput
              v-model:value="wizard.form.sshUser"
              placeholder="root"
              :input-props="{ id: 'add-server-ssh-user', 'aria-label': $t('servers.wizard.sshUser') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.sshUserHint") }}</span>
          </NFormItem>
        </div>
      </section>

      <section class="connect-group" :aria-label="$t('servers.wizard.connectAddress')">
        <h4 class="connect-group__title">{{ $t("servers.wizard.connectAddress") }}</h4>
        <div class="addr-row">
          <NFormItem
            :label="$t('servers.wizard.ipLabel')"
            path="ip"
            :label-props="{ for: 'add-server-ip' }"
          >
            <NInput
              v-model:value="wizard.form.ip"
              placeholder="203.0.113.90"
              :input-props="{ id: 'add-server-ip', 'aria-label': $t('servers.wizard.ipLabel') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.ipHint") }}</span>
          </NFormItem>
          <NFormItem
            :label="$t('servers.wizard.portLabel')"
            path="port"
            :label-props="{ for: 'add-server-port' }"
          >
            <NInputNumber
              v-model:value="wizard.form.port"
              :min="1"
              :max="65535"
              placeholder="22"
              :input-props="{ id: 'add-server-port', 'aria-label': $t('servers.wizard.portLabel') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.portHint") }}</span>
          </NFormItem>
        </div>
      </section>

      <section class="connect-group" :aria-label="$t('servers.wizard.connectAccess')">
        <h4 class="connect-group__title">{{ $t("servers.wizard.connectAccess") }}</h4>
        <NFormItem :label="$t('servers.wizard.authLabel')">
          <NRadioGroup
            v-model:value="wizard.form.authMode"
            size="small"
            :aria-label="$t('servers.wizard.authMethod')"
          >
            <NRadioButton value="managed">{{ $t("servers.wizard.authManaged") }}</NRadioButton>
            <NRadioButton value="manual">{{ $t("servers.wizard.authManual") }}</NRadioButton>
          </NRadioGroup>
          <span class="field-hint">{{ $t("servers.wizard.authHint") }}</span>
        </NFormItem>
      </section>

      <section class="connect-group" :aria-label="$t('servers.wizard.connectCredentials')">
        <h4 class="connect-group__title">{{ $t("servers.wizard.connectCredentials") }}</h4>

        <template v-if="wizard.form.authMode === 'managed'">
          <NFormItem
            :label="$t('servers.wizard.keySelectLabel')"
            path="keyId"
            :label-props="{ for: 'add-server-key-id' }"
          >
            <div class="key-picker">
              <NSelect
                v-model:value="wizard.form.keyId"
                :options="keyOptions"
                :loading="wizard.keysLoading.value"
                :placeholder="$t('servers.wizard.keySelectPlaceholder')"
                :aria-label="$t('servers.wizard.keySelectLabel')"
                :input-props="{ id: 'add-server-key-id' }"
              />
              <NButton
                secondary
                :loading="wizard.generatingKey.value"
                :disabled="wizard.keysLoading.value"
                @click="wizard.generateKey"
              >
                {{ $t("servers.wizard.keyGenerate") }}
              </NButton>
            </div>
            <span class="field-hint">{{ $t("servers.wizard.keySelectHint") }}</span>
          </NFormItem>
        </template>

        <template v-else>
          <NFormItem
            :label="$t('servers.wizard.privateKey')"
            path="privateKey"
            :label-props="{ for: 'add-server-private-key' }"
          >
            <NInput
              v-model:value="wizard.form.privateKey"
              type="textarea"
              :autosize="{ minRows: 4, maxRows: 10 }"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              :input-props="{ id: 'add-server-private-key', 'aria-label': $t('servers.wizard.privateKey') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.privateKeyHint") }}</span>
          </NFormItem>
          <NFormItem
            :label="$t('servers.wizard.publicKey')"
            path="publicKey"
            :label-props="{ for: 'add-server-public-key' }"
          >
            <NInput
              v-model:value="wizard.form.publicKey"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              placeholder="ssh-ed25519 AAAA…"
              :input-props="{ id: 'add-server-public-key', 'aria-label': $t('servers.wizard.publicKey') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.publicKeyHint") }}</span>
          </NFormItem>
        </template>

        <NAlert v-if="wizard.keyErrorMessage.value" type="error" :show-icon="true">
          {{ wizard.keyErrorMessage.value }}
        </NAlert>
      </section>
    </div>
  </NForm>
</template>

<style scoped>
.key-picker {
  display: flex;
  gap: var(--space-2);
  width: 100%;
  align-items: center;
}

.key-picker :deep(.n-select) {
  flex: 1;
  min-width: 0;
}

.key-picker :deep(.n-button) {
  flex: 0 0 auto;
}
</style>
