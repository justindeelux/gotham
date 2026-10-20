<script setup lang="ts">
import { Plus } from "@lucide/vue";
import { NButton, NModal } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import QuickCommandEditor from "@/features/terminal/components/QuickCommandEditor.vue";
import QuickCommandRow from "@/features/terminal/components/QuickCommandRow.vue";
import { QUICK_COMMANDS_MAX } from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand, QuickCommandDraft } from "@/features/terminal/schemas/quickCommands";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalCommandFailure } from "@/features/terminal/transport/terminalRuntime";

/**
 * Quick-command manager modal (JUS-264). The list, editor and every action
 * live in one bounded Naive UI modal: the header and footer stay fixed and
 * only the body scrolls (shared `.app-modal` contract). Inserting writes the
 * command to the active PTY without Enter, closes the modal and lets the
 * surface return focus to the terminal.
 */
interface Props {
  show: boolean;
  tabId: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  inserted: [label: string];
  closed: [];
}>();
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
const atLimit = computed<boolean>(() => store.commands.length >= QUICK_COMMANDS_MAX);

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

/** close requests the modal to hide; after-leave owns focus restoration. */
function close(): void {
  emit("update:show", false);
}

/** startAdd opens a blank editor. */
function startAdd(): void {
  if (atLimit.value) {
    return;
  }
  editingId.value = "";
  formError.value = "";
  insertError.value = "";
  showForm.value = true;
}

/** startEdit opens the editor on one saved command. */
function startEdit(command: QuickCommand): void {
  editingId.value = command.id;
  formError.value = "";
  insertError.value = "";
  showForm.value = true;
}

/** resetForm closes the editor without saving. */
function resetForm(): void {
  showForm.value = false;
  editingId.value = "";
  formError.value = "";
}

/** saveCommand persists a validated draft and updates the list immediately. */
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

/** insertCommand puts the command on the prompt without running it. */
function insertCommand(command: QuickCommand): void {
  const result = terminal.insertCommand(props.tabId, command.command);
  if (!result.ok) {
    insertError.value = failureText(result.failure);
    return;
  }
  insertError.value = "";
  emit("inserted", command.label);
  close();
}

/** removeCommand deletes one saved command from this browser. */
function removeCommand(id: string): void {
  store.remove(id);
  if (editingId.value === id) {
    resetForm();
  }
}

// A reopened modal starts on the list, never on a stale editor.
watch(
  () => props.show,
  (visible) => {
    if (!visible) {
      resetForm();
      insertError.value = "";
    }
  },
);
</script>

<template>
  <NModal
    class="app-modal term-quick-commands"
    :show="props.show"
    preset="card"
    style="width: min(36rem, 94vw)"
    :title="t('terminal.quickCommands.title')"
    :mask-closable="false"
    :auto-focus="true"
    :trap-focus="true"
    :close-on-esc="true"
    @update:show="(value: boolean) => emit('update:show', value)"
    @after-leave="emit('closed')"
  >
    <div class="qc" role="group" :aria-label="t('terminal.quickCommands.title')">
      <div class="qc-summary">
        <span class="qc-count">
          {{ t("terminal.quickCommands.count", { count: store.commands.length }) }}
        </span>
        <span class="qc-note">{{ t("terminal.quickCommands.storageNote") }}</span>
      </div>

      <p v-if="store.saveFailed" class="qc-alert" role="alert">
        {{ t("terminal.quickCommands.saveFailed") }}
      </p>

      <div v-if="store.commands.length === 0" class="qc-empty">
        <p>{{ t("terminal.quickCommands.empty") }}</p>
        <p>
          {{
            t("terminal.quickCommands.emptyExample", {
              shortcut: "dcup",
            })
          }}
        </p>
      </div>
      <ul v-else class="qc-list">
        <QuickCommandRow
          v-for="command in store.commands"
          :key="command.id"
          :command="command"
          @insert="insertCommand(command)"
          @edit="startEdit(command)"
          @delete="removeCommand(command.id)"
        />
      </ul>

      <p v-if="insertError" class="qc-alert" role="alert">{{ insertError }}</p>

      <QuickCommandEditor
        v-if="showForm"
        :key="editingId === '' ? 'new' : editingId"
        :commands="store.commands"
        :editing="editing"
        :error="formError"
        @save="saveCommand"
        @cancel="resetForm"
      />

      <p v-if="atLimit" class="qc-note">{{ t("terminal.quickCommands.atLimit") }}</p>
    </div>

    <template #footer>
      <div class="qc-foot">
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
        <span class="qc-foot-spacer" />
        <NButton size="small" class="qc-close" @click="close">
          {{ t("terminal.quickCommands.done") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.qc {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}

.qc-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-2);
}

.qc-count {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--fg-2);
  font-size: var(--text-sm);
  font-weight: 600;
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
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-3) 0;
  color: var(--meta);
  font-size: var(--text-xs);
  text-align: center;
}

.qc-empty p {
  margin: 0;
}

.qc-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.qc-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.qc-foot-spacer {
  flex: 1 1 auto;
}
</style>
