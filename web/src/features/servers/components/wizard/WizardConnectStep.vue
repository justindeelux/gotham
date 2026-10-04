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
        This node is already registered. Changing the connection
        details here does not update it — delete and re-add the node to
        change them. Continue without creating a duplicate.
      </NAlert>

      <section class="connect-group" aria-label="Identity">
        <h4 class="connect-group__title">Identity</h4>
        <div class="form-row">
          <NFormItem
            label="Node name"
            path="name"
            :label-props="{ for: 'add-server-name' }"
          >
            <NInput
              v-model:value="wizard.form.name"
              placeholder="build-node-03"
              :input-props="{ id: 'add-server-name', 'aria-label': 'Node name' }"
            />
            <span class="field-hint">A short unique name, e.g. build-node-03.</span>
          </NFormItem>
          <NFormItem
            label="SSH user"
            path="sshUser"
            :label-props="{ for: 'add-server-ssh-user' }"
          >
            <NInput
              v-model:value="wizard.form.sshUser"
              placeholder="root"
              :input-props="{ id: 'add-server-ssh-user', 'aria-label': 'SSH user' }"
            />
            <span class="field-hint">The Unix user the control plane connects as.</span>
          </NFormItem>
        </div>
      </section>

      <section class="connect-group" aria-label="Address">
        <h4 class="connect-group__title">Address</h4>
        <div class="addr-row">
          <NFormItem
            label="IP address / hostname"
            path="ip"
            :label-props="{ for: 'add-server-ip' }"
          >
            <NInput
              v-model:value="wizard.form.ip"
              placeholder="203.0.113.90"
              :input-props="{ id: 'add-server-ip', 'aria-label': 'IP address or hostname' }"
            />
            <span class="field-hint">IPv4 or hostname, e.g. 203.0.113.90 or node3.internal.</span>
          </NFormItem>
          <NFormItem
            label="SSH port"
            path="port"
            :label-props="{ for: 'add-server-port' }"
          >
            <NInputNumber
              v-model:value="wizard.form.port"
              :min="1"
              :max="65535"
              placeholder="22"
              :input-props="{ id: 'add-server-port', 'aria-label': 'SSH port' }"
            />
            <span class="field-hint">Usually 22.</span>
          </NFormItem>
        </div>
      </section>

      <section class="connect-group" aria-label="Access">
        <h4 class="connect-group__title">Access</h4>
        <NFormItem label="Authentication">
          <NRadioGroup
            v-model:value="wizard.form.authMode"
            size="small"
            aria-label="Authentication method"
          >
            <NRadioButton value="key">SSH key</NRadioButton>
            <NRadioButton value="password">Password</NRadioButton>
          </NRadioGroup>
          <span class="field-hint">Authenticate with a stored private key or a node password.</span>
        </NFormItem>
        <NFormItem v-if="wizard.form.authMode === 'key'" label="SSH key">
          <NRadioGroup
            v-model:value="wizard.form.keyMode"
            size="small"
            aria-label="SSH key mode"
          >
            <NRadioButton value="new">Paste a new key</NRadioButton>
            <NRadioButton value="existing">Use an existing key ID</NRadioButton>
          </NRadioGroup>
          <span class="field-hint">Key listing is not exposed by the API yet — paste the key material or a known key ID.</span>
        </NFormItem>
      </section>

      <section class="connect-group" aria-label="Credentials">
        <h4 class="connect-group__title">Credentials</h4>

        <template v-if="wizard.form.authMode === 'key' && wizard.form.keyMode === 'new'">
          <NFormItem
            label="Key name"
            path="keyName"
            :label-props="{ for: 'add-server-key-name' }"
          >
            <NInput
              v-model:value="wizard.form.keyName"
              placeholder="deploy-key"
              :input-props="{ id: 'add-server-key-name', 'aria-label': 'Key name' }"
            />
            <span class="field-hint">A label so you can reuse the key for other nodes.</span>
          </NFormItem>
          <NFormItem
            label="Private key (PEM)"
            path="privateKey"
            :label-props="{ for: 'add-server-private-key' }"
          >
            <NInput
              v-model:value="wizard.form.privateKey"
              type="textarea"
              :autosize="{ minRows: 4, maxRows: 10 }"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              :input-props="{ id: 'add-server-private-key', 'aria-label': 'Private key (PEM)' }"
            />
            <span class="field-hint">Ed25519 or RSA in PEM format. Stored encrypted, never returned.</span>
          </NFormItem>
          <NFormItem
            label="Key passphrase (if any)"
            path="passphrase"
            :label-props="{ for: 'add-server-passphrase' }"
          >
            <NInput
              v-model:value="wizard.form.passphrase"
              type="password"
              show-password-on="click"
              placeholder="Leave empty for unencrypted keys"
              :input-props="{ id: 'add-server-passphrase', 'aria-label': 'Key passphrase' }"
            />
            <span class="field-hint">Required only for a passphrase-protected key. Sent for validation, never stored.</span>
          </NFormItem>
        </template>

        <NFormItem
          v-else-if="wizard.form.authMode === 'key'"
          label="Key ID"
          path="keyId"
          :label-props="{ for: 'add-server-key-id' }"
        >
          <NInput
            v-model:value="wizard.form.keyId"
            placeholder="00000000-0000-0000-0000-000000000000"
            :input-props="{ id: 'add-server-key-id', 'aria-label': 'Key ID' }"
          />
          <span class="field-hint">The UUID of a key already stored on the control plane.</span>
        </NFormItem>

        <template v-else>
          <NFormItem
            label="Node password"
            path="password"
            :label-props="{ for: 'add-server-password' }"
          >
            <NInput
              v-model:value="wizard.form.password"
              type="password"
              show-password-on="click"
              placeholder="Node SSH password"
              :input-props="{ id: 'add-server-password', 'aria-label': 'Node password', autocomplete: 'new-password' }"
            />
            <span class="field-hint">Stored encrypted, never returned. Sent over SSH for validation.</span>
          </NFormItem>
        </template>
      </section>

      <section
        v-if="wizard.form.authMode === 'password'"
        class="connect-group"
        aria-label="Trust"
      >
        <h4 class="connect-group__title">Trust</h4>
        <NFormItem label="First connection" :show-feedback="false">
          <NCheckbox v-model:checked="wizard.form.trustHostKey">
            Trust this host key on first validation
          </NCheckbox>
          <span class="field-hint">Required once: a password node has no key to pin until it is trusted.</span>
        </NFormItem>
      </section>
    </div>
  </NForm>
</template>
