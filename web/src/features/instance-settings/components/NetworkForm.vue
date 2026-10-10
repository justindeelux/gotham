<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NRadioButton, NRadioGroup, NSwitch } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveNetwork } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import { useSectionForm } from "@/features/instance-settings/composables/useSectionForm";
import {
  dnsListSchema,
  ipv4AddressSchema,
  ipv4GatewaySchema,
  ipv6AddressSchema,
  ipv6GatewaySchema,
  splitList,
} from "@/features/instance-settings/schemas/instance";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ state: InstanceState }>();
const emit = defineEmits<{ saved: [state: InstanceState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const form = reactive({
  dns: "",
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
    form.dns = n.dns_servers.join(", ");
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
  dns: ruleFrom(dnsListSchema),
  v4Address: ruleFrom(ipv4AddressSchema, { when: v4Static }),
  v4Gateway: ruleFrom(ipv4GatewaySchema, { when: v4Static }),
  v6Address: ruleFrom(ipv6AddressSchema, { when: v6Static }),
  v6Gateway: ruleFrom(ipv6GatewaySchema, { when: v6Static }),
};

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveNetwork({
      dns_servers: splitList(form.dns),
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

const disabled = (): boolean => !props.state.capabilities.network || props.state.pending !== null;
</script>

<template>
  <NCard :title="t('instance-settings.network.title')">
    <NAlert v-if="!state.capabilities.network" type="info" :show-icon="true">
      {{ t("instance-settings.unsupported") }}
    </NAlert>
    <NAlert v-else-if="state.pending" type="warning" :show-icon="true">
      {{ t("instance-settings.network.pendingNote") }}
    </NAlert>
    <NAlert v-if="errorMessage" type="error" :show-icon="true">{{ errorMessage }}</NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="instance-form" @submit.prevent="handleSubmit">
      <NFormItem
        path="dns"
        :label="t('instance-settings.network.dns')"
        :validation-status="status('dns_servers')"
        :feedback="serverErrors.dns_servers"
      >
        <NInput v-model:value="form.dns" :disabled="disabled()" placeholder="1.1.1.1, 2606:4700:4700::1111" />
        <template #feedback v-if="!serverErrors.dns_servers">
          {{ t("instance-settings.network.dnsHint") }}
        </template>
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

h4 {
  margin: 8px 0;
}
</style>
