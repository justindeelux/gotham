import { ref } from "vue";
import type { Ref } from "vue";

import { useAuthStore } from "@/features/auth/stores/auth";

export function useRegisterInvite(): {
  inviteToken: Ref<string>;
  inviteEmail: Ref<string>;
  inviteChecking: Ref<boolean>;
  holdToken: (_token: string) => void;
  acceptInvite: () => Promise<boolean>;
} {
  const authStore = useAuthStore();

  const inviteToken = ref("");
  const inviteEmail = ref("");
  const inviteChecking = ref(false);

  function holdToken(token: string): void {
    inviteToken.value = token;
    inviteChecking.value = true;
  }

  async function acceptInvite(): Promise<boolean> {
    try {
      const info = await authStore.validateInvite(inviteToken.value);
      inviteEmail.value = info.email;
      return true;
    } catch {
      return false;
    } finally {
      inviteChecking.value = false;
    }
  }

  return { inviteToken, inviteEmail, inviteChecking, holdToken, acceptInvite };
}
