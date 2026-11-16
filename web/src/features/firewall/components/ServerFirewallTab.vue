<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NFormItem,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  useMessage,
} from "naive-ui";
import type { DataTableColumns, SelectOption } from "naive-ui";
import { computed, h, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import type { FirewallRule } from "../api/firewall";
import { useFirewall } from "../composables/useFirewall";
import { firewallRuleFormSchema } from "../schemas/firewall";
import { errorText } from "@/shared/api/errors";
import { firstIssueMessage, ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ serverId: string }>();

const { t } = useI18n();
const message = useMessage();
const firewall = useFirewall(() => props.serverId);

interface FormState {
  action: string;
  direction: string;
  protocol: string;
  port: string;
  source: string;
  comment: string;
  enabled: boolean;
}

function blankForm(): FormState {
  return {
    action: "allow",
    direction: "in",
    protocol: "tcp",
    port: "",
    source: "",
    comment: "",
    enabled: true,
  };
}

const form = reactive<FormState>(blankForm());
const formOpen = ref(false);
const editingId = ref<string | null>(null);
const formError = ref("");
const submitting = ref(false);
const toggleError = ref("");

const formRules = {
  action: ruleFrom(firewallRuleFormSchema.shape.action, { required: true }),
  direction: ruleFrom(firewallRuleFormSchema.shape.direction, { required: true }),
  protocol: ruleFrom(firewallRuleFormSchema.shape.protocol, { required: true }),
  port: ruleFrom(firewallRuleFormSchema.shape.port),
  source: ruleFrom(firewallRuleFormSchema.shape.source),
  comment: ruleFrom(firewallRuleFormSchema.shape.comment),
};

const actionOptions = computed<SelectOption[]>(() => [
  { label: t("firewall.rules.action.allow"), value: "allow" },
  { label: t("firewall.rules.action.deny"), value: "deny" },
]);

const directionOptions = computed<SelectOption[]>(() => [
  { label: t("firewall.rules.direction.in"), value: "in" },
  { label: t("firewall.rules.direction.out"), value: "out" },
]);

const protocolOptions = computed<SelectOption[]>(() => [
  { label: t("firewall.rules.protocol.tcp"), value: "tcp" },
  { label: t("firewall.rules.protocol.udp"), value: "udp" },
  { label: t("firewall.rules.protocol.any"), value: "any" },
]);

function portText(rule: FirewallRule): string {
  return rule.port === "" ? t("firewall.rules.any") : rule.port;
}

function sourceText(rule: FirewallRule): string {
  return rule.source === "" ? t("firewall.rules.any") : rule.source;
}

function ruleTarget(rule: FirewallRule): string {
  return rule.port === ""
    ? t("firewall.rules.targetAll")
    : t("firewall.rules.targetPort", { port: rule.port });
}

function accessPortText(ports: number | number[]): string {
  const list = Array.isArray(ports) ? ports : [ports];
  return list.filter((port) => port > 0).join(", ") || "—";
}

const accessHint = computed<string>(() => {
  const status = firewall.status.value;
  if (!status) {
    return "";
  }
  const ports = {
    ssh: accessPortText(status.ssh_ports),
    agent: accessPortText(status.agent_port),
    controlPlane: accessPortText(status.control_plane_port),
  };
  return status.control_plane_port > 0
    ? t("firewall.status.accessHint", ports)
    : t("firewall.status.accessHintRemote", ports);
});

const columns = computed<DataTableColumns<FirewallRule>>(() => [
  {
    title: t("firewall.rules.columns.status"),
    key: "status",
    width: 150,
    render: (rule) =>
      h(NSpace, { size: 4, wrap: true }, {
        default: () => {
          const tags = [
            h(
              NTag,
              { size: "small", bordered: false, type: rule.enabled ? "success" : "default" },
              {
                default: () =>
                  rule.enabled ? t("firewall.rules.enabled") : t("firewall.rules.disabled"),
              },
            ),
          ];
          if (rule.protected) {
            tags.push(
              h(
                NTag,
                { size: "small", bordered: false, type: "warning" },
                { default: () => t("firewall.rules.required") },
              ),
            );
          }
          if (rule.read_only) {
            tags.push(
              h(
                NTag,
                { size: "small", bordered: false, type: "info" },
                { default: () => t("firewall.rules.readOnly") },
              ),
            );
          }
          return tags;
        },
      }),
  },
  {
    title: t("firewall.rules.columns.action"),
    key: "action",
    width: 110,
    render: (rule) =>
      h(
        NTag,
        {
          size: "small",
          bordered: false,
          type: rule.action === "allow" ? "success" : "error",
        },
        { default: () => t(`firewall.rules.action.${rule.action}`) },
      ),
  },
  {
    title: t("firewall.rules.columns.direction"),
    key: "direction",
    width: 100,
    render: (rule) => t(`firewall.rules.direction.${rule.direction}`),
  },
  {
    title: t("firewall.rules.columns.protocol"),
    key: "protocol",
    width: 100,
    render: (rule) => t(`firewall.rules.protocol.${rule.protocol}`),
  },
  {
    title: t("firewall.rules.columns.port"),
    key: "port",
    width: 140,
    render: (rule) => h("span", { class: "num" }, portText(rule)),
  },
  {
    title: t("firewall.rules.columns.source"),
    key: "source",
    width: 170,
    render: (rule) => h("span", { class: "mono" }, sourceText(rule)),
  },
  {
    title: t("firewall.rules.columns.comment"),
    key: "comment",
    minWidth: 160,
    render: (rule) => rule.comment || h("span", { class: "muted" }, "—"),
  },
  {
    title: t("firewall.rules.columns.actions"),
    key: "actions",
    width: 170,
    render: (rule) => {
      const locked = rule.read_only || rule.protected;
      const hint = rule.read_only
        ? t("firewall.rules.readOnlyHint")
        : rule.protected
          ? t("firewall.rules.protectedHint")
          : "";
      return h(NSpace, { size: 4, wrap: false }, {
        default: () => [
          h(
            NButton,
            {
              size: "small",
              quaternary: true,
              disabled: locked,
              title: hint,
              onClick: () => openEdit(rule),
            },
            { default: () => t("firewall.rules.edit") },
          ),
          h(
            NPopconfirm,
            {
              positiveText: t("firewall.rules.delete"),
              negativeText: t("firewall.form.cancel"),
              disabled: locked,
              onPositiveClick: () => void removeRule(rule),
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: "small",
                    quaternary: true,
                    type: "error",
                    disabled: locked,
                    title: hint,
                  },
                  { default: () => t("firewall.rules.delete") },
                ),
              default: () => t("firewall.rules.deleteConfirm", { target: ruleTarget(rule) }),
            },
          ),
        ],
      });
    },
  },
]);

function openCreate(): void {
  Object.assign(form, blankForm());
  editingId.value = null;
  formError.value = "";
  formOpen.value = true;
}

function openEdit(rule: FirewallRule): void {
  Object.assign(form, {
    action: rule.action,
    direction: rule.direction,
    protocol: rule.protocol,
    port: rule.port,
    source: rule.source,
    comment: rule.comment,
    enabled: rule.enabled,
  });
  editingId.value = rule.id;
  formError.value = "";
  formOpen.value = true;
}

async function handleToggle(value: boolean): Promise<void> {
  toggleError.value = "";
  try {
    await firewall.toggle(value);
    message.success(value ? t("firewall.toasts.enabled") : t("firewall.toasts.disabled"));
  } catch (error) {
    toggleError.value = errorText(error) || t("firewall.errors.toggle");
  }
}

async function handleSubmit(): Promise<void> {
  const parsed = firewallRuleFormSchema.safeParse(form);
  if (!parsed.success) {
    formError.value = firstIssueMessage(parsed.error);
    return;
  }
  formError.value = "";
  submitting.value = true;
  try {
    await firewall.save(parsed.data, editingId.value ?? undefined);
    message.success(t("firewall.toasts.saved"));
    formOpen.value = false;
  } catch (error) {
    formError.value = errorText(error) || t("firewall.errors.save");
  } finally {
    submitting.value = false;
  }
}

async function removeRule(rule: FirewallRule): Promise<void> {
  try {
    await firewall.remove(rule.id);
    message.success(t("firewall.toasts.deleted"));
  } catch (error) {
    message.error(errorText(error) || t("firewall.errors.delete"));
  }
}

onMounted(() => {
  void firewall.load();
});
</script>

<template>
  <NSpin :show="firewall.loading.value">
    <NAlert v-if="firewall.loadError.value" type="error" :show-icon="true">
      <NSpace vertical :size="8">
        <span>{{ firewall.loadError.value || t("firewall.errors.load") }}</span>
        <NButton size="small" @click="firewall.load">
          {{ t("firewall.status.reload") }}
        </NButton>
      </NSpace>
    </NAlert>

    <NCard
      v-else-if="firewall.status.value && !firewall.status.value.manageable"
      :title="t('firewall.status.unsupportedTitle')"
    >
      <NEmpty :description="firewall.status.value.detail || t('firewall.status.unsupportedBody')">
        <template #extra>
          <NButton size="small" @click="firewall.load">
            {{ t("firewall.status.reload") }}
          </NButton>
        </template>
      </NEmpty>
    </NCard>

    <NSpace v-else-if="firewall.status.value" vertical :size="16">
      <NCard :title="t('firewall.title')">
        <template #header-extra>
          <NSpace align="center" :size="8">
            <span class="muted">
              {{
                firewall.status.value.enabled
                  ? t("firewall.status.on")
                  : t("firewall.status.off")
              }}
            </span>
            <NSwitch
              :value="firewall.status.value.enabled"
              :loading="firewall.saving.value"
              @update:value="handleToggle"
            />
          </NSpace>
        </template>

        <NSpace vertical :size="12">
          <NSpace align="center" :size="8">
            <span class="muted">{{ t("firewall.status.backend") }}</span>
            <NTag size="small" :bordered="false" type="info">
              {{ firewall.status.value.backend }}
            </NTag>
          </NSpace>
          <p class="firewall-hint">{{ accessHint }}</p>
          <NAlert
            v-if="!firewall.status.value.enabled"
            type="info"
            :show-icon="true"
          >
            {{ t("firewall.status.turnOnHint") }}
          </NAlert>
          <NAlert v-if="toggleError" type="error" :show-icon="true">
            {{ toggleError }}
          </NAlert>
          <p class="firewall-description">{{ t("firewall.description") }}</p>
        </NSpace>
      </NCard>

      <NCard :title="t('firewall.rules.title')">
        <template #header-extra>
          <NButton size="small" type="primary" @click="openCreate">
            {{ t("firewall.rules.add") }}
          </NButton>
        </template>

        <NEmpty
          v-if="!firewall.loading.value && firewall.rules.value.length === 0"
          :description="t('firewall.rules.empty')"
        >
          <template #extra>
            <span class="muted">{{ t("firewall.rules.emptyHint") }}</span>
          </template>
        </NEmpty>
        <NDataTable
          v-else
          :columns="columns"
          :data="firewall.rules.value"
          :row-key="(rule: FirewallRule) => rule.id"
          :bordered="false"
          :scroll-x="980"
        />
      </NCard>
    </NSpace>

    <NModal
      :show="formOpen"
      preset="card"
      class="app-modal standard-width"
      :title="
        editingId === null
          ? t('firewall.form.addTitle')
          : t('firewall.form.editTitle')
      "
      :mask-closable="false"
      :close-on-esc="!submitting"
      @update:show="(value: boolean) => { if (!submitting) formOpen = value; }"
    >
      <NSpace vertical :size="14">
        <NAlert v-if="formError" type="error" :show-icon="true">
          {{ formError }}
        </NAlert>

        <div class="firewall-form-grid">
          <NFormItem
            :label="t('firewall.form.action')"
            :rule="formRules.action"
            :label-props="{ for: 'firewall-rule-action' }"
          >
            <NSelect
              v-model:value="form.action"
              :options="actionOptions"
              :input-props="{ id: 'firewall-rule-action' }"
            />
          </NFormItem>
          <NFormItem
            :label="t('firewall.form.direction')"
            :rule="formRules.direction"
            :label-props="{ for: 'firewall-rule-direction' }"
          >
            <NSelect
              v-model:value="form.direction"
              :options="directionOptions"
              :input-props="{ id: 'firewall-rule-direction' }"
            />
          </NFormItem>
          <NFormItem
            :label="t('firewall.form.protocol')"
            :rule="formRules.protocol"
            :label-props="{ for: 'firewall-rule-protocol' }"
          >
            <NSelect
              v-model:value="form.protocol"
              :options="protocolOptions"
              :input-props="{ id: 'firewall-rule-protocol' }"
            />
          </NFormItem>
          <NFormItem
            :label="t('firewall.form.port')"
            :rule="formRules.port"
            :label-props="{ for: 'firewall-rule-port' }"
          >
            <NInput
              v-model:value="form.port"
              :placeholder="t('firewall.form.portPlaceholder')"
              :input-props="{ id: 'firewall-rule-port', autocomplete: 'off' }"
            />
          </NFormItem>
          <NFormItem
            :label="t('firewall.form.source')"
            :rule="formRules.source"
            :label-props="{ for: 'firewall-rule-source' }"
          >
            <NInput
              v-model:value="form.source"
              :placeholder="t('firewall.form.sourcePlaceholder')"
              :input-props="{ id: 'firewall-rule-source', autocomplete: 'off' }"
            />
          </NFormItem>
          <NFormItem
            :label="t('firewall.form.comment')"
            :rule="formRules.comment"
            :label-props="{ for: 'firewall-rule-comment' }"
          >
            <NInput
              v-model:value="form.comment"
              :placeholder="t('firewall.form.commentPlaceholder')"
              :input-props="{ id: 'firewall-rule-comment', autocomplete: 'off' }"
              maxlength="200"
            />
          </NFormItem>
        </div>

        <p class="firewall-hint">
          {{ t("firewall.form.portHint") }} {{ t("firewall.form.sourceHint") }}
        </p>

        <NFormItem :label="t('firewall.form.enabled')">
          <NSpace align="center" :size="8">
            <NSwitch
              v-model:value="form.enabled"
              :aria-label="t('firewall.form.enabled')"
            />
            <span class="muted">{{ t("firewall.form.enabledHint") }}</span>
          </NSpace>
        </NFormItem>
      </NSpace>

      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton :disabled="submitting" @click="formOpen = false">
            {{ t("firewall.form.cancel") }}
          </NButton>
          <NButton type="primary" :loading="submitting" @click="handleSubmit">
            {{ t("firewall.form.save") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </NSpin>
</template>

<style scoped>
.firewall-hint {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
}

.firewall-description {
  margin: 0;
  color: var(--meta);
  font-size: var(--text-sm);
}

.firewall-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-3) var(--space-4);
}

@media (max-width: 640px) {
  .firewall-form-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
