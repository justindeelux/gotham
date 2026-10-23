import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { serviceNameSchema, serviceNodeSchema } from "@/shared/validation/primitives";
import servicesEn from "@/features/services/locales/en";
import servicesVi from "@/features/services/locales/vi";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { services: servicesEn });
  i18n.global.mergeLocaleMessage("vi", { services: servicesVi });
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  setLocale("en", null);
});

describe("service schemas keep the exact dialog messages", () => {
  it("rejects blank names and nodes with the recorded strings", () => {
    expect(fieldErrors(serviceNameSchema, "  ")[0]).toBe("Enter a service name.");
    expect(fieldErrors(serviceNameSchema, "")[0]).toBe("Enter a service name.");
    expect(fieldErrors(serviceNameSchema, "blog-staging")).toEqual([]);
    expect(fieldErrors(serviceNodeSchema, "")[0]).toBe("Select a node.");
    expect(fieldErrors(serviceNodeSchema, "srv-1")).toEqual([]);
  });

  it("renders the same refusals in Vietnamese without changing gating", () => {
    setLocale("vi", null);
    expect(fieldErrors(serviceNameSchema, "  ")[0]).toBe("Nhập tên dịch vụ.");
    expect(fieldErrors(serviceNodeSchema, "")[0]).toBe("Chọn một node.");
    expect(fieldErrors(serviceNameSchema, "blog-staging")).toEqual([]);
    expect(fieldErrors(serviceNodeSchema, "srv-1")).toEqual([]);
  });
});
