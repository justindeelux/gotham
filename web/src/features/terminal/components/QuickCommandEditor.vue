<script setup lang="ts">
import { NButton, NForm, NFormItem, NInput } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  QUICK_COMMAND_ABBREVIATION_MAX,
  QUICK_COMMAND_LABEL_MAX,
  QUICK_COMMAND_TEXT_MAX,
  quickCommandAbbreviationFieldSchema,
  quickCommandDraftSchema,
  quickCommandLabelSchema,
  quickCommandTextSchema,
} from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand, QuickCommandDraft } from "@/features/terminal/schemas/quickCommands";
import { firstIssueMessage, ruleFrom } from "@/shared/validation/naiveAdapter";

/**
 * Add/edit form for one quick command (JUS-249). It validates through Naive
 * rules and the same zod draft schema the store enforces; a duplicate
 * abbreviation and a control/bidi character are rejected before saving.
 */
interface Props {
  commands: QuickCommand[];
  editing: QuickCommand | null;
  error?: string;
}

const props = withDefaults(defineProps<Props>(), { error: "" });
const emit = defineEmits<{ save: [draft: QuickCommandDraft]; cancel: [] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const labelRef = ref<{ focus: () => void } | null>(null);
const formError = ref("");

const form = reactive<{ label: string; command: string; abbreviation: string }>({
  label: props.editing?.label ?? "",
  command: props.editing?.command ?? "",
  abbreviation: props.editing?.abbreviation ?? "",
});

const rules = computed<FormRules>(() => ({
  label: ruleFrom(quickCommandLabelSchema, { required: true }),
  command: ruleFrom(quickCommandTextSchema, { required: true }),
  abbreviation: ruleFrom(
    quickCommandAbbreviationFieldSchema(
      props.commands,
      props.editing?.id ?? "",
    ),
  ),
}));

const shownError = computed<string>(() => props.error || formError.value);

/** save validates through Naive then through the shared zod draft schema. */
async function save(): Promise<void> {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  const draft: QuickCommandDraft = {
    label: form.label,
    command: form.command,
    ...(form.abbreviation.trim() !== ""
      ? { abbreviation: form.abbreviation }
      : {}),
  };
  const parsed = quickCommandDraftSchema(
    props.commands,
    props.editing?.id ?? "",
  ).safeParse(draft);
  if (!parsed.success) {
    formError.value = firstIssueMessage(parsed.error);
    return;
  }
  formError.value = "";
  emit("save", parsed.data);
}

onMounted(() => {
  labelRef.value?.focus();
});
</script>

<template>
  <NForm
    ref="formRef"
    :model="form"
    :rules="rules"
    class="qc-form"
    @submit.prevent="save"
  >
    <p class="qc-form-title">
      {{
        props.editing
          ? t("terminal.quickCommands.editTitle")
          : t("terminal.quickCommands.addTitle")
      }}
    </p>
    <NFormItem path="label" :label="t('terminal.quickCommands.label')">
      <NInput
        ref="labelRef"
        v-model:value="form.label"
        :maxlength="QUICK_COMMAND_LABEL_MAX"
        :placeholder="t('terminal.quickCommands.labelPlaceholder')"
      />
    </NFormItem>
    <NFormItem path="command" :label="t('terminal.quickCommands.command')">
      <NInput
        v-model:value="form.command"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 5 }"
        :maxlength="QUICK_COMMAND_TEXT_MAX"
        :placeholder="t('terminal.quickCommands.commandPlaceholder')"
      />
    </NFormItem>
    <NFormItem path="abbreviation" :label="t('terminal.quickCommands.abbreviation')">
      <NInput
        v-model:value="form.abbreviation"
        :maxlength="QUICK_COMMAND_ABBREVIATION_MAX"
        :placeholder="t('terminal.quickCommands.abbreviationPlaceholder')"
      />
    </NFormItem>
    <p class="qc-note qc-abbr-hint">
      {{ t("terminal.quickCommands.abbreviationHint") }}
    </p>
    <p v-if="shownError" class="qc-alert" role="alert">{{ shownError }}</p>
    <div class="qc-form-actions">
      <NButton size="small" type="primary" attr-type="submit">
        {{ t("terminal.quickCommands.save") }}
      </NButton>
      <NButton size="small" secondary @click="emit('cancel')">
        {{ t("terminal.quickCommands.cancel") }}
      </NButton>
    </div>
  </NForm>
</template>

<style scoped>
.qc-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding-top: var(--space-1);
  border-top: 1px solid var(--border);
}

.qc-form-title {
  margin: 0;
  padding-top: var(--space-2);
  color: var(--fg-2);
  font-size: var(--text-sm);
  font-weight: 600;
}

.qc-form-actions {
  display: flex;
  gap: var(--space-2);
}

.qc-note {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.45;
}

.qc-abbr-hint {
  margin-top: calc(-1 * var(--space-2));
}

.qc-alert {
  margin: 0;
  color: var(--danger);
  font-size: var(--text-xs);
}
</style>
