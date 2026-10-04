// Differential tests for the teams zod migration (V9 sweep, JUS-23).
// The two disabled states pin the old trim-and-empty gating, including the
// kept non-check (any non-empty invite string passes, no email-format rule).
// The domains sweep found no client-side field checks (the server owns
// redirect/certificate/provider validation), so no domains schema is kept.
import { describe, expect, it } from "vitest";

import {
  isInviteEmailValid,
  isTeamNameValid,
} from "@/features/teams/schemas/teams";

describe("teams gating matches the trim-and-empty disabled states", () => {
  it("team name gates like createName/renameName", () => {
    for (const value of ["", "   ", undefined, null]) {
      expect(isTeamNameValid(value)).toBe(false);
    }
    for (const value of ["Core", "  Core  ", "a"]) {
      expect(isTeamNameValid(value)).toBe(true);
    }
  });
  it("invite gates on non-empty only, with no format check", () => {
    for (const value of ["", "   ", undefined, null]) {
      expect(isInviteEmailValid(value)).toBe(false);
    }
    for (const value of ["name@example.com", "  name@example.com  ", "not-an-email"]) {
      expect(isInviteEmailValid(value)).toBe(true);
    }
  });
});
