<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NRadioButton, NRadioGroup, NSwitch } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveNetwork } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import { useSectionForm } from "@/features/instance-settings/composables/useSectionForm";
import {
  dnsPrimaryServerSchema,
  dnsServerSchema,
  dnsServersValid,
  ipv4AddressSchema,
  ipv4GatewaySchema,
  ipv6AddressSchema,
  ipv6GatewaySchema,
} from "@/features/instance-settings/schemas/instance";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ state: InstanceState }>();
const emit = defineEmits<{ saved: [state: InstanceState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const dnsExtra = ref(false);
const form = reactive({
  dns0: "",
  dns1: "",
  dns2: "",
  v4Mode: "dhcp" as "dhcp" | "static",
  v4Address: "",
  v4Gateway: "",
  v6Enabled: true,
  v6Mode: "dhcp" as "dhcp" | "static",
  v6Address: "",
  v6Gateway: "",
});

watch(
  () => props.state.network,
  (n) => {
    // A stored third server stays editable; fewer than two pad to Primary/Alternate.
    const servers = n.dns_servers.slice(0, 3);
    form.dns0 = servers[0] ?? "";
    form.dns1 = servers[1] ?? "";
    form.dns2 = servers[2] ?? "";
    dnsExtra.value = servers.length > 2;
    form.v4Mode = n.ipv4.mode;
    form.v4Address = n.ipv4.address;
    form.v4Gateway = n.ipv4.gateway;
    form.v6Enabled = n.ipv6.enabled;
    form.v6Mode = n.ipv6.mode;
    form.v6Address = n.ipv6.address;
    form.v6Gateway = n.ipv6.gateway;
  },
  { immediate: true },
);

const v4Static = (): boolean => form.v4Mode === "static";
const v6Static = (): boolean => form.v6Enabled && form.v6Mode === "static";

const rules: FormRules = {
  dns0: ruleFrom(dnsPrimaryServerSchema, { required: true }),
  dns1: ruleFrom(dnsServerSchema),
  dns2: ruleFrom(dnsServerSchema, { when: () => dnsExtra.value }),
  v4Address: ruleFrom(ipv4AddressSchema, { when: v4Static }),
  v4Gateway: ruleFrom(ipv4GatewaySchema, { when: v4Static }),
  v6Address: ruleFrom(ipv6AddressSchema, { when: v6Static }),
  v6Gateway: ruleFrom(ipv6GatewaySchema, { when: v6Static }),
};

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveNetwork({
      dns_servers: dnsServers(),
      ipv4: {
        mode: form.v4Mode,
        address: v4Static() ? form.v4Address.trim() : "",
        gateway: v4Static() ? form.v4Gateway.trim() : "",
      },
      ipv6: {
        enabled: form.v6Enabled,
        mode: form.v6Enabled ? form.v6Mode : "dhcp",
        address: v6Static() ? form.v6Address.trim() : "",
        gateway: v6Static() ? form.v6Gateway.trim() : "",
      },
    }),
  (next) => emit("saved", next),
  () => t("instance-settings.network.applied"),
);

/** dnsServers collects the visible resolver inputs, dropping empties. */
function dnsServers(): string[] {
  const inputs = dnsExtra.value ? [form.dns0, form.dns1, form.dns2] : [form.dns0, form.dns1];
  return inputs.map((item) => item.trim()).filter((item) => item !== "");
}

async function handleSubmit(): Promise<void> {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  if (!dnsServersValid(dnsServers())) {
    errorMessage.value = t("instance-settings.validation.dns");
    return;
  }
  await submit();
}

function status(path: string): "error" | undefined {
  return serverErrors.value[path] ? "error" : undefined;
}

const disabled = (): boolean => !props.state.capabilities.network || props.state.pending !== null;
</script>

<template>
  <NCard :title="t('instance-settings.network.title')">
    <NAlert v-if="!state.capabilities.network" type="info" :show-icon="true" class="form-alert">
      {{ t("instance-settings.unsupported") }}
    </NAlert>
    <NAlert v-else-if="state.pending" type="warning" :show-icon="true" class="form-alert">
      {{ t("instance-settings.network.pendingNote") }}
    </NAlert>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert">{{ errorMessage }}</NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="instance-form" @submit.prevent="handleSubmit">
      <NFormItem
        path="dns0"
        :label="t('instance-settings.network.dnsPrimary')"
        :validation-status="status('dns_servers')"
        :feedback="serverErrors.dns_servers"
      >
        <NInput v-model:value="form.dns0" :disabled="disabled()" placeholder="1.1.1.1" />
      </NFormItem>
      <NFormItem
        path="dns1"
        :label="`${t('instance-settings.network.dnsAlternate')} (${t('instance-settings.network.optional')})`"
      >
        <NInput v-model:value="form.dns1" :disabled="disabled()" placeholder="2606:4700:4700::1111" />
        <span class="field-hint">
          {{ t("instance-settings.network.dnsHint") }}
          <NButton
            v-if="!dnsExtra"
            text
            type="primary"
            size="small"
            :disabled="disabled()"
            @click="dnsExtra = true"
          >
            {{ t("instance-settings.network.dnsAdd") }}
          </NButton>
        </span>
      </NFormItem>
      <NFormItem
        v-if="dnsExtra"
        path="dns2"
        :label="t('instance-settings.network.dnsExtra', { n: 3 })"
      >
        <NInput v-model:value="form.dns2" :disabled="disabled()" placeholder="9.9.9.9" />
      </NFormItem>

      <h4>IPv4</h4>
      <NFormItem :label="t('instance-settings.network.mode')">
        <NRadioGroup v-model:value="form.v4Mode" :disabled="disabled()">
          <NRadioButton value="dhcp">{{ t("instance-settings.network.dhcp") }}</NRadioButton>
          <NRadioButton value="static">{{ t("instance-settings.network.static") }}</NRadioButton>
        </NRadioGroup>
      </NFormItem>
      <template v-if="form.v4Mode === 'static'">
        <NFormItem
          path="v4Address"
          :label="t('instance-settings.network.address')"
          :validation-status="status('ipv4.address')"
          :feedback="serverErrors['ipv4.address']"
        >
          <NInput v-model:value="form.v4Address" :disabled="disabled()" placeholder="192.168.1.10/24" />
        </NFormItem>
        <NFormItem
          path="v4Gateway"
          :label="t('instance-settings.network.gateway')"
          :validation-status="status('ipv4.gateway')"
          :feedback="serverErrors['ipv4.gateway']"
        >
          <NInput v-model:value="form.v4Gateway" :disabled="disabled()" placeholder="192.168.1.1" />
        </NFormItem>
      </template>

      <h4>IPv6</h4>
      <NFormItem :label="t('instance-settings.network.ipv6Enabled')">
        <NSwitch v-model:value="form.v6Enabled" :disabled="disabled()" />
      </NFormItem>
      <template v-if="form.v6Enabled">
        <NFormItem :label="t('instance-settings.network.mode')">
          <NRadioGroup v-model:value="form.v6Mode" :disabled="disabled()">
            <NRadioButton value="dhcp">{{ t("instance-settings.network.auto") }}</NRadioButton>
            <NRadioButton value="static">{{ t("instance-settings.network.static") }}</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <template v-if="form.v6Mode === 'static'">
          <NFormItem
            path="v6Address"
            :label="t('instance-settings.network.address')"
            :validation-status="status('ipv6.address')"
            :feedback="serverErrors['ipv6.address']"
          >
            <NInput v-model:value="form.v6Address" :disabled="disabled()" placeholder="2001:db8::10/64" />
          </NFormItem>
          <NFormItem
            path="v6Gateway"
            :label="t('instance-settings.network.gateway')"
            :validation-status="status('ipv6.gateway')"
            :feedback="serverErrors['ipv6.gateway']"
          >
            <NInput v-model:value="form.v6Gateway" :disabled="disabled()" placeholder="2001:db8::1" />
          </NFormItem>
        </template>
      </template>

      <NButton type="primary" :loading="submitting" :disabled="disabled()" @click="handleSubmit">
        {{ t("instance-settings.network.submit") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.instance-form {
  max-width: 640px;
}

/* JUS-99: hints live in the default slot (below their input). NFormItem
 * renders slot content in .n-form-item-blank, a flex row, so wrap it —
 * the same pattern main.css applies to the wizard/edit modals. */
.instance-form :deep(.n-form-item-blank) {
  flex-wrap: wrap;
}

h4 {
  margin: 8px 0;
}
</style>
