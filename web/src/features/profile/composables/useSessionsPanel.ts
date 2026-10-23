import { useMessage } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";
import {
  listSessions,
  revokeOtherSessions,
  revokeSession,
} from "@/features/profile/api/sessions";
import { profileMessages } from "@/features/profile/schemas/profile";
import type { AuthSession } from "@/features/profile/schemas/sessions";

export function useSessionsPanel() {
  const { t } = useI18n();
  const authStore = useAuthStore();
  const message = useMessage();
  const router = useRouter();

  const sessions = ref<AuthSession[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const errorKey = ref<string | null>(null);
  const errorMessage = computed<string>(() =>
    errorKey.value === null ? "" : t(errorKey.value),
  );
  const revokingId = ref<string | null>(null);
  const revokingOthers = ref(false);
  const needsReauth = ref(false);

  let listSeq = 0;

  const others = computed<AuthSession[]>(() =>
    sessions.value.filter((session) => !session.current),
  );

  async function load(): Promise<void> {
    const seq = ++listSeq;
    loading.value = true;
    errorKey.value = null;
    try {
      const list = await listSessions();
      if (seq !== listSeq) {
        return;
      }
      sessions.value = list;
      loaded.value = true;
    } catch {
      if (seq !== listSeq) {
        return;
      }
      errorKey.value = profileMessages.sessionsLoadFailed;
    } finally {
      if (seq === listSeq) {
        loading.value = false;
      }
    }
  }

  async function refreshList(seq: number): Promise<boolean> {
    try {
      const list = await listSessions();
      if (seq === listSeq) {
        sessions.value = list;
      }
      return true;
    } catch {
      return false;
    }
  }

  function statusOf(error: unknown): number | null {
    if (typeof error !== "object" || error === null) {
      return null;
    }
    const status = (error as { status?: unknown }).status;
    return typeof status === "number" ? status : null;
  }

  async function signOutHere(): Promise<void> {
    try {
      await authStore.logout();
    } catch {
    }
    await router.push({ name: "login" });
  }

  async function endSession(session: AuthSession): Promise<void> {
    if (revokingId.value !== null) {
      return;
    }
    revokingId.value = session.id;
    errorKey.value = null;
    const seq = ++listSeq;
    try {
      await revokeSession(session.id);
    } catch (error) {
      revokingId.value = null;
      if (statusOf(error) === 404) {
        await refreshList(seq);
        return;
      }
      errorKey.value = profileMessages.sessionEndFailed;
      return;
    }
    revokingId.value = null;
    if (session.current) {
      message.success(t("profile.sessions.signedOutHere"));
      await signOutHere();
      return;
    }
    message.success(t("profile.sessions.sessionSignedOut"));
    if (!(await refreshList(seq))) {
      sessions.value = sessions.value.filter((row) => row.id !== session.id);
      errorKey.value = profileMessages.sessionsListStale;
    }
  }

  async function endOtherSessions(): Promise<void> {
    if (revokingOthers.value) {
      return;
    }
    revokingOthers.value = true;
    errorKey.value = null;
    const seq = ++listSeq;
    try {
      await revokeOtherSessions();
    } catch (error) {
      revokingOthers.value = false;
      if (statusOf(error) === 409) {
        needsReauth.value = true;
        return;
      }
      errorKey.value = profileMessages.revokeOthersFailed;
      return;
    }
    revokingOthers.value = false;
    message.success(t("profile.sessions.signedOut"));
    if (!(await refreshList(seq))) {
      sessions.value = sessions.value.filter((row) => row.current);
      errorKey.value = profileMessages.sessionsListStale;
    }
  }

  return {
    sessions,
    others,
    loading,
    loaded,
    errorMessage,
    revokingId,
    revokingOthers,
    needsReauth,
    load,
    endSession,
    endOtherSessions,
    signOutHere,
  };
}
