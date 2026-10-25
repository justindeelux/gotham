import { useMessage } from "naive-ui";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { createToken, describeTokenError } from "@/features/api-tokens";
import type { CreatedToken } from "@/features/api-tokens";
import { fetchMcpConnection } from "@/features/mcp/api/connection";
import type { McpConnection } from "@/features/mcp/schemas/connection";
import { buildMcpSnippets } from "@/features/mcp/utils/snippets";

export type McpAccessLevel = "read" | "deploy";

// Token names only separate entries in the API tokens list; the date keeps
// repeated setups apart without asking the user for a name.
export function mcpTokenName(now: Date = new Date()): string {
  return `MCP setup ${now.toISOString().slice(0, 10)}`;
}

export function useMcpSetupPage() {
  const { t } = useI18n();
  const message = useMessage();

  const connection = ref<McpConnection | null>(null);
  const loading = ref(false);
  const failure = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    failure.value === null
      ? ""
      : describeTokenError(failure.value, t("mcp.errors.loadFailed")),
  );

  const accessLevel = ref<McpAccessLevel>("read");
  const createBusy = ref(false);
  const createFailure = ref<unknown>(null);
  const createErrorMessage = computed<string>(() =>
    createFailure.value === null
      ? ""
      : describeTokenError(createFailure.value, t("mcp.errors.createFailed")),
  );

  const revealed = ref<CreatedToken | null>(null);

  const snippets = computed(() =>
    buildMcpSnippets(connection.value?.mcp_url ?? "", null),
  );
  const revealedSnippets = computed(() =>
    buildMcpSnippets(
      connection.value?.mcp_url ?? "",
      revealed.value?.token ?? null,
    ),
  );

  async function load(): Promise<void> {
    loading.value = true;
    failure.value = null;
    try {
      connection.value = await fetchMcpConnection();
    } catch (error) {
      failure.value = error;
    } finally {
      loading.value = false;
    }
  }

  async function create(): Promise<void> {
    if (createBusy.value) {
      return;
    }
    createBusy.value = true;
    createFailure.value = null;
    try {
      revealed.value = await createToken(mcpTokenName(), accessLevel.value);
      message.success(t("mcp.toast.created"));
    } catch (error) {
      createFailure.value = error;
    } finally {
      createBusy.value = false;
    }
  }

  function closeReveal(): void {
    revealed.value = null;
  }

  onMounted(() => {
    void load();
  });

  // Leaving the page drops the plaintext token.
  onBeforeUnmount(closeReveal);

  return {
    connection,
    loading,
    errorMessage,
    accessLevel,
    createBusy,
    createErrorMessage,
    revealed,
    snippets,
    revealedSnippets,
    load,
    create,
    closeReveal,
  };
}
