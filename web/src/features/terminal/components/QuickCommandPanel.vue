<script setup lang="ts">
import { BookMarked, Copy, Pencil, Plus, Trash2 } from "@lucide/vue";
import {
  NButton,
  NForm,
  NFormItem,
  NInput,
  NPopconfirm,
  NTag,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, nextTick, reactive, ref } from "vue";
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
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import {
  isSupportedSuggestionShell,
  shellIntegrationSnippet,
} from "@/features/terminal/utils/quickCommandInsertion";
import type { TerminalCommandFailure } from "@/features/terminal/transport/terminalRuntime";
import { firstIssueMessage, ruleFrom } from "@/shared/validation/naiveAdapter";

/**
 * Quick-command list and editor (JUS-249). Mounted inside the terminal
 * surface's popover, so page and floating-window sessions share one editor.
 * Selecting a command inserts its text; it never presses Enter.
 */
interface Props {
  tabId: string;
  shell: string;
  integrationDetected: boolean;
  alternateScreen: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{ inserted: [label: string] }>();
const { t } = useI18n();
const store = useQuickCommandsStore();
const terminal = useTerminalStore();

const formRef = ref<FormInst | null>(null);
const labelRef = ref<{ focus: () => void } | null>(null);
const showForm = ref(false);
const editingId = ref("");
const formError = ref("");
const insertError = ref("");
const copied = ref(false);

const form = reactive<{ label: string; command: string; abbreviation: string }>({
  label: "",
  command: "",
  abbreviation: "",
});

/** rules validate the same zod schemas the store enforces on save. */
const rules = computed<FormRules>(() => ({
  label: ruleFrom(quickCommandLabelSchema, { required: true }),
  command: ruleFrom(quickCommandTextSchema, { required: true }),
  abbreviation: ruleFrom(
    quickCommandAbbreviationFieldSchema(
      store.commands,
      editingId.value,
    ),
  ),
}));

const snippet = computed<string>(() => shellIntegrationSnippet(props.shell));
const supported = computed<boolean>(() => isSupportedSuggestionShell(props.shell));
const hintVisible = computed<boolean>(() => !props.integrationDetected);

/** failureText maps an insertion refusal onto localized guidance. */
function failureText(failure?: TerminalCommandFailure): string {
  switch (failure) {
    case "multiline-no-paste":
      return t("terminal.quickCommands.insertMultiline");
    case "control":
      return t("terminal.quickCommands.insertControl");
    case "empty":
      return t("terminal.quickCommands.insertEmpty");
    default:
      return t("terminal.quickCommands.insertUnavailable");
  }
}

/** startAdd opens a blank editor. */
function startAdd(): void {
  editingId.value = "";
  form.label = "";
  form.command = "";
  form.abbreviation = "";
  formError.value = "";
  showForm.value = true;
  void nextTick(() => labelRef.value?.focus());
}

/** startEdit opens the editor on one saved command. */
function startEdit(command: QuickCommand): void {
  editingId.value = command.id;
  form.label = command.label;
  form.command = command.command;
  form.abbreviation = command.abbreviation ?? "";
  formError.value = "";
  showForm.value = true;
  void nextTick(() => labelRef.value?.focus());
}

/** resetForm closes the editor without saving. */
function resetForm(): void {
  showForm.value = false;
  editingId.value = "";
  formError.value = "";
}

/** save validates through Naive then through the store's zod schema. */
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
    store.commands,
    editingId.value,
  ).safeParse(draft);
  if (!parsed.success) {
    formError.value = firstIssueMessage(parsed.error);
    return;
  }
  const saved =
    editingId.value === ""
      ? store.add(parsed.data) !== null
      : store.update(editingId.value, parsed.data);
  if (!saved) {
    formError.value = t("terminal.quickCommands.saveRejected");
    return;
  }
  resetForm();
}

/** insertText puts the command on the prompt without running it. */
function insertText(command: QuickCommand): void {
  const result = terminal.insertCommand(props.tabId, command.command);
  if (!result.ok) {
    insertError.value = failureText(result.failure);
    return;
  }
  insertError.value = "";
  emit("inserted", command.label);
}

/** removeCommand deletes one saved command from this browser. */
function removeCommand(id: string): void {
  store.remove(id);
  if (editingId.value === id) {
    resetForm();
  }
}

/** copySnippet copies the optional shell integration snippet. */
async function copySnippet(): Promise<void> {
  const clipboard =
    typeof navigator === "undefined" ? undefined : navigator.clipboard;
  if (!clipboard || snippet.value === "") {
    return;
  }
  try {
    await clipboard.writeText(snippet.value);
    copied.value = true;
  } catch {
    copied.value = false;
  }
}
</script>

<template>
  <div class="qc" role="group" :aria-label="t('terminal.quickCommands.title')">
    <header class="qc-head">
      <BookMarked :size="15" :stroke-width="1.8" aria-hidden="true" />
      <h3 class="qc-title">{{ t("terminal.quickCommands.title") }}</h3>
      <span class="qc-count">
        {{ t("terminal.quickCommands.count", { count: store.commands.length }) }}
      </span>
    </header>

    <p class="qc-note">{{ t("terminal.quickCommands.storageNote") }}</p>
    <p v-if="store.saveFailed" class="qc-alert" role="alert">
      {{ t("terminal.quickCommands.saveFailed") }}
    </p>

    <p v-if="store.commands.length === 0" class="qc-empty">
      {{ t("terminal.quickCommands.empty") }}
    </p>
    <ul v-else class="qc-list">
      <li v-for="command in store.commands" :key="command.id" class="qc-item">
        <div class="qc-main">
          <div class="qc-line">
            <span class="qc-label">{{ command.label }}</span>
            <NTag v-if="command.abbreviation" size="tiny" type="info" class="mono">
              {{ command.abbreviation }}
            </NTag>
          </div>
          <code class="qc-text mono" :title="command.command">{{ command.command }}</code>
        </div>
        <div class="qc-actions">
          <NButton
            size="tiny"
            secondary
            class="qc-insert"
            :aria-label="t('terminal.quickCommands.insertLabel', { label: command.label })"
            @click="insertText(command)"
          >
            {{ t("terminal.quickCommands.insert") }}
          </NButton>
          <NButton
            quaternary
            size="tiny"
            :aria-label="t('terminal.quickCommands.editLabel', { label: command.label })"
            @click="startEdit(command)"
          >
            <template #icon>
              <Pencil :size="13" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
          <NPopconfirm @positive-click="removeCommand(command.id)">
            <template #trigger>
              <NButton
                quaternary
                size="tiny"
                :aria-label="t('terminal.quickCommands.deleteLabel', { label: command.label })"
              >
                <template #icon>
                  <Trash2 :size="13" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
            </template>
            <span class="qc-confirm">
              {{ t("terminal.quickCommands.deleteBody", { label: command.label }) }}
            </span>
          </NPopconfirm>
        </div>
      </li>
    </ul>

    <p v-if="insertError" class="qc-alert" role="alert">{{ insertError }}</p>

    <NButton v-if="!showForm" size="small" secondary class="qc-add" @click="startAdd">
      <template #icon>
        <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
      {{ t("terminal.quickCommands.add") }}
    </NButton>

    <NForm
      v-else
      ref="formRef"
      :model="form"
      :rules="rules"
      class="qc-form"
      @submit.prevent="save"
    >
      <p class="qc-form-title">
        {{ editingId === "" ? t("terminal.quickCommands.addTitle") : t("terminal.quickCommands.editTitle") }}
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
      <p v-if="formError" class="qc-alert" role="alert">{{ formError }}</p>
      <div class="qc-form-actions">
        <NButton size="small" type="primary" attr-type="submit">
          {{ t("terminal.quickCommands.save") }}
        </NButton>
        <NButton size="small" secondary @click="resetForm">
          {{ t("terminal.quickCommands.cancel") }}
        </NButton>
      </div>
    </NForm>

    <section v-if="hintVisible" class="qc-hint">
      <p class="qc-hint-title">{{ t("terminal.quickCommands.hintTitle") }}</p>
      <p class="qc-note">{{ t("terminal.quickCommands.hintBody") }}</p>
      <p v-if="props.alternateScreen" class="qc-note">
        {{ t("terminal.quickCommands.hintAlternate") }}
      </p>
      <template v-if="supported">
        <pre class="qc-snippet mono">{{ snippet }}</pre>
        <NButton size="tiny" secondary class="qc-copy" @click="copySnippet">
          <template #icon>
            <Copy :size="13" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ copied ? t("terminal.quickCommands.copied") : t("terminal.quickCommands.copySnippet") }}
        </NButton>
      </template>
      <p v-else class="qc-note">{{ t("terminal.quickCommands.hintUnsupported") }}</p>
    </section>
  </div>
</template>

<style scoped>
.qc {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: min(22rem, calc(100vw - 7rem));
  max-height: min(70vh, 34rem);
  overflow-y: auto;
  padding: var(--space-1);
}

.qc-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--fg-2);
}

.qc-title {
  margin: 0;
  font-size: var(--text-sm);
  font-weight: 600;
}

.qc-count {
  margin-left: auto;
  color: var(--meta);
  font-size: var(--text-xs);
}

.qc-note {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.45;
}

.qc-alert {
  margin: 0;
  color: var(--danger);
  font-size: var(--text-xs);
}

.qc-empty {
  margin: 0;
  padding: var(--space-3) 0;
  color: var(--meta);
  font-size: var(--text-xs);
  text-align: center;
}

.qc-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.qc-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
}

.qc-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1 1 auto;
}

.qc-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.qc-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.qc-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.qc-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex: 0 0 auto;
}

.qc-confirm {
  display: block;
  max-width: 14rem;
  font-size: var(--text-xs);
}

.qc-add {
  align-self: flex-start;
}

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

.qc-abbr-hint {
  margin-top: calc(-1 * var(--space-2));
}

.qc-hint {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding-top: var(--space-2);
  border-top: 1px solid var(--border);
}

.qc-hint-title {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-xs);
  font-weight: 600;
}

.qc-snippet {
  margin: 0;
  padding: var(--space-2);
  max-height: 12rem;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  color: var(--fg-2);
  font-size: var(--text-xs);
  line-height: 1.4;
  white-space: pre;
}

.qc-copy {
  align-self: flex-start;
}
</style>
