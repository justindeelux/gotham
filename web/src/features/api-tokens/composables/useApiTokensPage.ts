import { useMessage } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import {
  createToken,
  describeTokenError,
  listTokens,
  revokeToken,
  rotateToken,
} from "@/features/api-tokens/api/tokens";
import { createTokenFormSchema } from "@/features/api-tokens/schemas/tokens";
import type {
  ApiToken,
  CreatedToken,
  TokenScope,
} from "@/features/api-tokens/schemas/tokens";

export function useApiTokensPage() {
  const { t } = useI18n();
  const authStore = useAuthStore();
  const message = useMessage();

  const tokens = ref<ApiToken[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const failure = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    failure.value === null
      ? ""
      : describeTokenError(failure.value, t("api-tokens.errors.loadFailed")),
  );

  const createOpen = ref(false);
  const createName = ref("");
  const createScope = ref<TokenScope>("read");
  const createTouched = ref(false);
  const createBusy = ref(false);
  const createFailure = ref<unknown>(null);
  const createErrorMessage = computed<string>(() =>
    createFailure.value === null
      ? ""
      : describeTokenError(
          createFailure.value,
          t("api-tokens.errors.createFailed"),
        ),
  );
  const createNameError = computed<string | null>(() => {
    if (!createTouched.value) {
      return null;
    }
    const result = createTokenFormSchema.safeParse({
      name: createName.value,
      scope: createScope.value,
    });
    if (result.success) {
      return null;
    }
    const issue = result.error.issues.find((item) => item.path[0] === "name");
    return issue ? String(t(issue.message)) : null;
  });

  const scopeOptions = computed<TokenScope[]>(() =>
    authStore.isOwner ? ["read", "deploy", "admin"] : ["read", "deploy"],
  );

  const revealed = ref<CreatedToken | null>(null);
  const rotatingId = ref<string | null>(null);
  const revokingId = ref<string | null>(null);

  async function refresh(): Promise<void> {
    loading.value = true;
    failure.value = null;
    try {
      tokens.value = await listTokens();
      loaded.value = true;
    } catch (error) {
      failure.value = error;
    } finally {
      loading.value = false;
    }
  }

  function openCreate(): void {
    createName.value = "";
    createScope.value = "read";
    createTouched.value = false;
    createFailure.value = null;
    createOpen.value = true;
  }

  function closeCreate(): void {
    createOpen.value = false;
    createTouched.value = false;
    createFailure.value = null;
  }

  function touchCreate(): void {
    createTouched.value = true;
  }

  async function submitCreate(): Promise<void> {
    createTouched.value = true;
    const parsed = createTokenFormSchema.safeParse({
      name: createName.value,
      scope: createScope.value,
    });
    if (createBusy.value || !parsed.success) {
      return;
    }
    createBusy.value = true;
    createFailure.value = null;
    try {
      revealed.value = await createToken(parsed.data.name, parsed.data.scope);
      createOpen.value = false;
      message.success(t("api-tokens.toast.created"));
      await refresh();
    } catch (error) {
      createFailure.value = error;
    } finally {
      createBusy.value = false;
    }
  }

  function closeReveal(): void {
    revealed.value = null;
  }

  async function rotate(token: ApiToken): Promise<void> {
    if (rotatingId.value !== null) {
      return;
    }
    rotatingId.value = token.id;
    try {
      revealed.value = await rotateToken(token.id);
      message.success(t("api-tokens.toast.rotated"));
      await refresh();
    } catch (error) {
      message.error(
        describeTokenError(error, t("api-tokens.errors.rotateFailed")),
      );
    } finally {
      rotatingId.value = null;
    }
  }

  async function revoke(token: ApiToken): Promise<void> {
    if (revokingId.value !== null) {
      return;
    }
    revokingId.value = token.id;
    try {
      await revokeToken(token.id);
      message.success(t("api-tokens.toast.revoked"));
      await refresh();
    } catch (error) {
      message.error(
        describeTokenError(error, t("api-tokens.errors.revokeFailed")),
      );
    } finally {
      revokingId.value = null;
    }
  }

  onMounted(() => {
    void refresh();
  });

  return {
    tokens,
    loading,
    loaded,
    errorMessage,
    createOpen,
    createName,
    createScope,
    createTouched,
    createBusy,
    createNameError,
    createErrorMessage,
    scopeOptions,
    revealed,
    rotatingId,
    revokingId,
    refresh,
    openCreate,
    closeCreate,
    touchCreate,
    submitCreate,
    closeReveal,
    rotate,
    revoke,
  };
}
