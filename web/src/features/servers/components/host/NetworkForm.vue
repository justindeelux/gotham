<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NForm,
  NFormItem,
  NInput,
  NRadioButton,
  NRadioGroup,
  NSwitch,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveNodeNetwork } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { useHostUnlock } from "@/features/servers/composables/useHostUnlock";
import {
  dnsPrimaryServerSchema,
  dnsServerSchema,
  dnsServersValid,
  ipv4AddressSchema,
  ipv4GatewaySchema,
  ipv6AddressSchema,
  ipv6GatewaySchema,
  isRiskyInterfaceChange,
  sameInterface,
} from "@/features/servers/schemas/host";
import type { InterfaceConfig } from "@/features/servers/schemas/host";
import { useSectionForm } from "@/shared/composables/useSectionForm";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ serverId: string; state: NodeHostState }>();
const emit = defineEmits<{ saved: [serverId: string, state: NodeHostState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const dnsExtra = ref(false);
const baseline = ref<InterfaceConfig>(snapshot(props.state.network));
const confirmChecked = ref(false);
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
    const servers = n.dns_servers.slice(0, 3);
    form.dns0 = servers[0] ?? "";
    form.dns1 = servers[1] ?? "";
    form.dns2 = servers[2] ?? "";
    dnsExtra.value = servers.length > 2;
    form.v4Mode = n.ipv4.mode === "static" ? "static" : "dhcp";
    form.v4Address = n.ipv4.address;
    form.v4Gateway = n.ipv4.gateway;
    form.v6Enabled = n.ipv6.enabled;
    form.v6Mode = n.ipv6.mode === "static" ? "static" : "dhcp";
    form.v6Address = n.ipv6.address;
    form.v6Gateway = n.ipv6.gateway;
    baseline.value = snapshot(n);
    confirmChecked.value = false;
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
    saveNodeNetwork(props.serverId, {
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
      confirm_interface_change: confirmChecked.value,
    }),
  (next) => emit("saved", props.serverId, next),
  () => t("servers.settings.host.network.applied"),
);

function snapshot(n: InterfaceConfig): InterfaceConfig {
  return {
    ipv4: { mode: n.ipv4.mode, address: n.ipv4.address, gateway: n.ipv4.gateway },
    ipv6: {
      enabled: n.ipv6.enabled,
      mode: n.ipv6.mode,
      address: n.ipv6.address,
      gateway: n.ipv6.gateway,
    },
  };
}

function current(): InterfaceConfig {
  return {
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
  };
}

const risky = computed<boolean>(() => isRiskyInterfaceChange(baseline.value, current()));
const dnsOnly = computed<boolean>(() => !risky.value && sameInterface(baseline.value, current()));

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
    errorMessage.value = t("servers.settings.host.validation.dns");
    return;
  }
  if (risky.value && !confirmChecked.value) {
    errorMessage.value = t("servers.settings.host.network.confirmRequired");
    return;
  }
  await submit();
}

function status(path: string): "error" | undefined {
  return serverErrors.value[path] ? "error" : undefined;
}

const disabled = (): boolean =>
  props.state.capability !== "ready" || props.state.pending !== null;

const { focusTarget, unlocked } = useHostUnlock(() => props.state.capability === "ready");
</script>

<template>
  <NCard :title="t('servers.settings.host.network.title')">
    <p class="sr-only" role="status" aria-live="polite">
      {{ unlocked ? t("servers.settings.host.helper.installed") : "" }}
    </p>
    <NAlert v-if="state.pending" type="warning" :show-icon="true" class="form-alert">
      {{ t("servers.settings.host.network.pendingNote") }}
    </NAlert>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert form-alert-multiline">{{ errorMessage }}</NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="host-form" @submit.prevent="handleSubmit">
      <NFormItem
        path="dns0"
        :label="t('servers.settings.host.network.dnsPrimary')"
        :validation-status="status('dns_servers')"
        :feedback="serverErrors.dns_servers"
      >
        <NInput ref="focusTarget" v-model:value="form.dns0" :disabled="disabled()" placeholder="1.1.1.1" />
      </NFormItem>
      <NFormItem
        path="dns1"
        :label="`${t('servers.settings.host.network.dnsAlternate')} (${t('servers.settings.host.network.optional')})`"
      >
        <NInput v-model:value="form.dns1" :disabled="disabled()" placeholder="1.0.0.1" />
        <span class="field-hint">
          {{ t("servers.settings.host.network.dnsHint") }}
          <NButton
            v-if="!dnsExtra"
            text
            type="primary"
            size="small"
            :disabled="disabled()"
            @click="dnsExtra = true"
          >
            {{ t("servers.settings.host.network.dnsAdd") }}
          </NButton>
        </span>
      </NFormItem>
      <NFormItem
        v-if="dnsExtra"
        path="dns2"
        :label="t('servers.settings.host.network.dnsExtra', { n: 3 })"
      >
        <NInput v-model:value="form.dns2" :disabled="disabled()" placeholder="9.9.9.9" />
      </NFormItem>

      <h4>IPv4</h4>
      <NFormItem :label="t('servers.settings.host.network.mode')">
        <NRadioGroup v-model:value="form.v4Mode" :disabled="disabled()">
          <NRadioButton value="dhcp">{{ t("servers.settings.host.network.dhcp") }}</NRadioButton>
          <NRadioButton value="static">{{ t("servers.settings.host.network.static") }}</NRadioButton>
        </NRadioGroup>
      </NFormItem>
      <template v-if="form.v4Mode === 'static'">
        <NFormItem
          path="v4Address"
          :label="t('servers.settings.host.network.address')"
          :validation-status="status('ipv4.address')"
          :feedback="serverErrors['ipv4.address']"
        >
          <NInput v-model:value="form.v4Address" :disabled="disabled()" placeholder="192.168.1.10/24" />
        </NFormItem>
        <NFormItem
          path="v4Gateway"
          :label="t('servers.settings.host.network.gateway')"
          :validation-status="status('ipv4.gateway')"
          :feedback="serverErrors['ipv4.gateway']"
        >
          <NInput v-model:value="form.v4Gateway" :disabled="disabled()" placeholder="192.168.1.1" />
        </NFormItem>
      </template>

      <h4>IPv6</h4>
      <NFormItem :label="t('servers.settings.host.network.ipv6Enabled')">
        <NSwitch v-model:value="form.v6Enabled" :disabled="disabled()" />
      </NFormItem>
      <template v-if="form.v6Enabled">
        <NFormItem :label="t('servers.settings.host.network.mode')">
          <NRadioGroup v-model:value="form.v6Mode" :disabled="disabled()">
            <NRadioButton value="dhcp">{{ t("servers.settings.host.network.auto") }}</NRadioButton>
            <NRadioButton value="static">{{ t("servers.settings.host.network.static") }}</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <template v-if="form.v6Mode === 'static'">
          <NFormItem
            path="v6Address"
            :label="t('servers.settings.host.network.address')"
            :validation-status="status('ipv6.address')"
            :feedback="serverErrors['ipv6.address']"
          >
            <NInput v-model:value="form.v6Address" :disabled="disabled()" placeholder="2001:db8::10/64" />
          </NFormItem>
          <NFormItem
            path="v6Gateway"
            :label="t('servers.settings.host.network.gateway')"
            :validation-status="status('ipv6.gateway')"
            :feedback="serverErrors['ipv6.gateway']"
          >
            <NInput v-model:value="form.v6Gateway" :disabled="disabled()" placeholder="2001:db8::1" />
          </NFormItem>
        </template>
      </template>

      <NButton type="primary" :loading="submitting" :disabled="disabled()" @click="handleSubmit">
        {{ t("servers.settings.host.network.submit") }}
      </NButton>
      <NAlert v-if="dnsOnly" type="info" :show-icon="true" class="form-alert">
        {{ t("servers.settings.host.network.dnsOnlyNote") }}
      </NAlert>
      <NAlert v-if="risky" type="warning" :show-icon="true" class="form-alert">
        {{ t("servers.settings.host.network.riskyWarning") }}
      </NAlert>
      <NCheckbox v-if="risky" v-model:checked="confirmChecked" :disabled="disabled()">
        {{ t("servers.settings.host.network.confirmLabel") }}
      </NCheckbox>
    </NForm>
  </NCard>
</template>

<style scoped>
.host-form {
  max-width: 640px;
}

.host-form :deep(.n-form-item-blank) {
  flex-wrap: wrap;
}

h4 {
  margin: 8px 0;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
