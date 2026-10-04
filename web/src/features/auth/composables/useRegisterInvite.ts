import { ref } from "vue";
import type { Ref } from "vue";

import { useAuthStore } from "@/features/auth/stores/auth";

/**
 * Invite acceptance state for RegisterPage (P-A2): when the instance already
 * has an account, registration is closed and the page is reachable only
 * through an admin-created invite link (`/register?invite=<token>`). The
 * token is validated up front so the invitee sees which team they are
 * joining; an unusable token bounces to the sign-in form.
 */
export function useRegisterInvite(): {
  inviteToken: Ref<string>;
  inviteTeam: Ref<string>;
  inviteEmail: Ref<string>;
  inviteChecking: Ref<boolean>;
  holdToken: (_token: string) => void;
  acceptInvite: () => Promise<boolean>;
} {
  const authStore = useAuthStore();

  const inviteToken = ref("");
  const inviteTeam = ref("");
  const inviteEmail = ref("");
  const inviteChecking = ref(false);

  /**
   * holdToken takes the token from the URL and blocks submission BEFORE the
   * config request: on a slow connection the form is already visible, and a
   * submit during that window would otherwise omit the invite and answer 403.
   */
  function holdToken(token: string): void {
    inviteToken.value = token;
    inviteChecking.value = true;
  }

  /**
   * acceptInvite validates the held token, returning true with the team/email
   * filled in, or false when the invite is unusable.
   */
  async function acceptInvite(): Promise<boolean> {
    try {
      const info = await authStore.validateInvite(inviteToken.value);
      inviteTeam.value = info.team;
      inviteEmail.value = info.email;
      return true;
    } catch {
      return false;
    } finally {
      inviteChecking.value = false;
    }
  }

  return { inviteToken, inviteTeam, inviteEmail, inviteChecking, holdToken, acceptInvite };
}
