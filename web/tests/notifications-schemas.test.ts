// Differential tests for the V8 notifications zod migration (JUS-23).
// canSubmit rows pin the old boolean outcomes; recipient rows pin
// parseRecipients outputs, which recipientsSchema must reproduce exactly.
import { describe, expect, it } from "vitest";

import {
  canSubmitChannel,
  channelSubmitSchema,
  recipientsSchema,
} from "@/features/notifications/schemas/notifications";
import { parseRecipients } from "@/features/notifications/utils/channelHelpers";

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

describe("recipientsSchema matches parseRecipients", () => {
  const raws = [
    "a@x.io, b@x.io\nc@x.io  ,",
    "   ",
    "",
    "a,,b",
    "a b\tc\nd",
    "single@example.com",
  ];
  it("splits on /[\\s,]+/, trims, drops empties", () => {
    for (const raw of raws) {
      expect(recipientsSchema.parse(raw)).toEqual(parseRecipients(raw));
    }
    expect(recipientsSchema.parse("a@x.io, b@x.io\nc@x.io  ,")).toEqual([
      "a@x.io",
      "b@x.io",
      "c@x.io",
    ]);
    expect(recipientsSchema.parse("   ")).toEqual([]);
  });
});
