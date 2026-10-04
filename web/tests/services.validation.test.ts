// Differential test (JUS-23): service schemas must keep the exact import
// dialog messages recorded from the pre-migration code.
import { describe, expect, it } from "vitest";

import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { serviceNameSchema, serviceNodeSchema } from "@/shared/validation/primitives";

describe("service schemas keep the exact dialog messages", () => {
  it("rejects blank names and nodes with the recorded strings", () => {
    expect(fieldErrors(serviceNameSchema, "  ")[0]).toBe("Enter a service name.");
    expect(fieldErrors(serviceNameSchema, "")[0]).toBe("Enter a service name.");
    expect(fieldErrors(serviceNameSchema, "blog-staging")).toEqual([]);
    expect(fieldErrors(serviceNodeSchema, "")[0]).toBe("Select a node.");
    expect(fieldErrors(serviceNodeSchema, "srv-1")).toEqual([]);
  });
});
