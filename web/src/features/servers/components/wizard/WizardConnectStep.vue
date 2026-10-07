<script setup lang="ts">
import {
  NAlert,
  NCheckbox,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NRadioButton,
  NRadioGroup,
} from "naive-ui";
import type { FormInst } from "naive-ui";
import { inject, onUnmounted, ref, watch } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";

const wizard = inject(WizardKey);
if (!wizard) {
  throw new Error("WizardConnectStep must be used inside AddServerWizard.");
}

// The NForm instance lives in the wizard context (create/validate/reset use
// it); a template string ref can only bind to this component's setup, so the
// local ref is mirrored into the context.
const formRef = ref<FormInst | null>(null);
watch(formRef, (instance) => {
  wizard.setFormRef(instance);
});
onUnmounted(() => {
  wizard.setFormRef(null);
});
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
            <NRadioButton value="key">{{ $t("servers.wizard.keyModeLabel") }}</NRadioButton>
            <NRadioButton value="password">{{ $t("servers.wizard.authPassword") }}</NRadioButton>
          </NRadioGroup>
          <span class="field-hint">{{ $t("servers.wizard.authHint") }}</span>
        </NFormItem>
        <NFormItem v-if="wizard.form.authMode === 'key'" :label="$t('servers.wizard.keyModeLabel')">
          <NRadioGroup
            v-model:value="wizard.form.keyMode"
            size="small"
            :aria-label="$t('servers.wizard.keyModeName')"
          >
            <NRadioButton value="new">{{ $t("servers.wizard.keyNew") }}</NRadioButton>
            <NRadioButton value="existing">{{ $t("servers.wizard.keyExisting") }}</NRadioButton>
          </NRadioGroup>
          <span class="field-hint">{{ $t("servers.wizard.keyModeHint") }}</span>
        </NFormItem>
      </section>

      <section class="connect-group" :aria-label="$t('servers.wizard.connectCredentials')">
        <h4 class="connect-group__title">{{ $t("servers.wizard.connectCredentials") }}</h4>

        <template v-if="wizard.form.authMode === 'key' && wizard.form.keyMode === 'new'">
          <NFormItem
            :label="$t('servers.wizard.keyName')"
            path="keyName"
            :label-props="{ for: 'add-server-key-name' }"
          >
            <NInput
              v-model:value="wizard.form.keyName"
              placeholder="deploy-key"
              :input-props="{ id: 'add-server-key-name', 'aria-label': $t('servers.wizard.keyName') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.keyNameHint") }}</span>
          </NFormItem>
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
            :label="$t('servers.wizard.passphrase')"
            path="passphrase"
            :label-props="{ for: 'add-server-passphrase' }"
          >
            <NInput
              v-model:value="wizard.form.passphrase"
              type="password"
              show-password-on="click"
              :placeholder="$t('servers.wizard.passphraseHint')"
              :input-props="{ id: 'add-server-passphrase', 'aria-label': $t('servers.wizard.passphrase') }"
            />
            <span class="field-hint">{{ $t("servers.wizard.passphraseHint") }}</span>
          </NFormItem>
        </template>

        <NFormItem
          v-else-if="wizard.form.authMode === 'key'"
          :label="$t('servers.wizard.keyId')"
          path="keyId"
          :label-props="{ for: 'add-server-key-id' }"
        >
          <NInput
            v-model:value="wizard.form.keyId"
            placeholder="00000000-0000-0000-0000-000000000000"
            :input-props="{ id: 'add-server-key-id', 'aria-label': $t('servers.wizard.keyId') }"
          />
          <span class="field-hint">{{ $t("servers.wizard.keyIdHint") }}</span>
        </NFormItem>

        <template v-else>
          <NFormItem
            :label="$t('servers.wizard.nodePassword')"
            path="password"
            :label-props="{ for: 'add-server-password' }"
          >
            <NInput
              v-model:value="wizard.form.password"
              type="password"
              show-password-on="click"
              :placeholder="$t('servers.wizard.nodePasswordHint')"
              :input-props="{ id: 'add-server-password', 'aria-label': $t('servers.wizard.nodePassword'), autocomplete: 'new-password' }"
            />
            <span class="field-hint">{{ $t("servers.wizard.nodePasswordHint") }}</span>
          </NFormItem>
        </template>
      </section>

      <section
        v-if="wizard.form.authMode === 'password'"
        class="connect-group"
        :aria-label="$t('servers.wizard.connectTrust')"
      >
        <h4 class="connect-group__title">{{ $t("servers.wizard.connectTrust") }}</h4>
        <NFormItem :label="$t('servers.wizard.firstConnection')" :show-feedback="false">
          <NCheckbox v-model:checked="wizard.form.trustHostKey">
            {{ $t("servers.wizard.trustHostKey") }}
          </NCheckbox>
          <span class="field-hint">{{ $t("servers.wizard.trustHint") }}</span>
        </NFormItem>
      </section>
    </div>
  </NForm>
</template>
