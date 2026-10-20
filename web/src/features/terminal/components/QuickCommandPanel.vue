<script setup lang="ts">
import { BookMarked, Plus } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import QuickCommandEditor from "@/features/terminal/components/QuickCommandEditor.vue";
import QuickCommandHint from "@/features/terminal/components/QuickCommandHint.vue";
import QuickCommandRow from "@/features/terminal/components/QuickCommandRow.vue";
import { QUICK_COMMANDS_MAX } from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand, QuickCommandDraft } from "@/features/terminal/schemas/quickCommands";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalCommandFailure } from "@/features/terminal/transport/terminalRuntime";

/**
 * Quick-command list and editor (JUS-249), mounted inside the terminal
 * surface's popover so page and floating-window sessions share one editor.
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

const showForm = ref(false);
const editingId = ref("");
const formError = ref("");
const insertError = ref("");

const editing = computed<QuickCommand | null>(
  () => store.commands.find((command) => command.id === editingId.value) ?? null,
);
const atLimit = computed<boolean>(
  () => store.commands.length >= QUICK_COMMANDS_MAX,
);
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
  if (atLimit.value) {
    return;
  }
  editingId.value = "";
  formError.value = "";
  showForm.value = true;
}

/** startEdit opens the editor on one saved command. */
function startEdit(command: QuickCommand): void {
  editingId.value = command.id;
  formError.value = "";
  showForm.value = true;
}

/** resetForm closes the editor without saving. */
function resetForm(): void {
  showForm.value = false;
  editingId.value = "";
  formError.value = "";
}

/** saveCommand persists a validated draft. */
function saveCommand(draft: QuickCommandDraft): void {
  if (editingId.value === "" && atLimit.value) {
    formError.value = t("terminal.quickCommands.atLimit");
    return;
  }
  const saved =
    editingId.value === ""
      ? store.add(draft) !== null
      : store.update(editingId.value, draft);
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
      <QuickCommandRow
        v-for="command in store.commands"
        :key="command.id"
        :command="command"
        @insert="insertText(command)"
        @edit="startEdit(command)"
        @delete="removeCommand(command.id)"
      />
    </ul>

    <p v-if="insertError" class="qc-alert" role="alert">{{ insertError }}</p>

    <NButton
      v-if="!showForm"
      size="small"
      secondary
      class="qc-add"
      :disabled="atLimit"
      @click="startAdd"
    >
      <template #icon>
        <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
      {{ t("terminal.quickCommands.add") }}
    </NButton>
    <p v-if="atLimit" class="qc-note">{{ t("terminal.quickCommands.atLimit") }}</p>

    <QuickCommandEditor
      v-if="showForm"
      :key="editingId === '' ? 'new' : editingId"
      :commands="store.commands"
      :editing="editing"
      :error="formError"
      @save="saveCommand"
      @cancel="resetForm"
    />

    <QuickCommandHint
      v-if="hintVisible"
      :shell="props.shell"
      :alternate-screen="props.alternateScreen"
    />
  </div>
</template>

<style scoped>
.qc {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: min(22rem, calc(100vw - 10rem));
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

.qc-add {
  align-self: flex-start;
}
</style>
