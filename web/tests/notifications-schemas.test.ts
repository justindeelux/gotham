// Differential tests for the V8 notifications zod migration (JUS-23).
// canSubmit rows pin the old boolean outcomes. Recipient splitting stays
// tested directly on parseRecipients in channel-helpers.test.ts.
import { describe, expect, it } from "vitest";

import {
  canSubmitChannel,
  channelSubmitSchema,
} from "@/features/notifications/schemas/notifications";

describe("canSubmitChannel matches canSubmit", () => {
  const rows: Array<
    [
      string,
      { name: string; events: string[]; resourceType: "" | "application" | "database"; resourceId: string },
      boolean,
    ]
  > = [
    ["blank name blocks", { name: "  ", events: ["deploy_success"], resourceType: "", resourceId: "" }, false],
    ["empty name blocks", { name: "", events: ["deploy_success"], resourceType: "", resourceId: "" }, false],
    ["padded name passes", { name: "  Ops  ", events: ["deploy_success"], resourceType: "", resourceId: "" }, true],
    ["no events blocks", { name: "n", events: [], resourceType: "", resourceId: "" }, false],
    ["team-wide needs no resource", { name: "n", events: ["deploy_success"], resourceType: "", resourceId: "" }, true],
    ["scoped needs a resource", { name: "n", events: ["deploy_success"], resourceType: "application", resourceId: "" }, false],
    ["scoped with resource passes", { name: "n", events: ["deploy_success"], resourceType: "application", resourceId: "app-1" }, true],
    ["database scope same rule", { name: "n", events: ["backup_success"], resourceType: "database", resourceId: "" }, false],
    ["team-wide ignores stale id", { name: "n", events: ["backup_success"], resourceType: "", resourceId: "db-1" }, true],
  ];
  it("gates exactly like the old computed", () => {
    for (const [label, form, valid] of rows) {
      expect(canSubmitChannel(form), label).toBe(valid);
      expect(channelSubmitSchema.safeParse(form).success, label).toBe(valid);
    }
  });
  it("ignores the non-gating draft keys", () => {
    const full = {
      name: "n",
      kind: "discord",
      enabled: true,
      events: ["deploy_success"],
      resourceType: "",
      resourceId: "",
      webhook_url: "",
      bot_token: "",
      chat_id: "",
      host: "",
      port: "587",
      username: "",
      password: "",
      from: "",
      to: "",
    };
    expect(canSubmitChannel(full)).toBe(true);
  });
});
