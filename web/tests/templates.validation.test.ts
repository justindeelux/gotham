// Differential test (JUS-23): templateFieldSchema must reproduce the
// recorded outcomes of the hand-written checkTemplateValue it replaces.
// Recorded 2026-10-04 from the pre-migration code; every row asserts
// identical outcome AND identical message string.
import { describe, expect, it } from "vitest";

import {
  checkTemplateValue,
  validateTemplateValues,
} from "@/features/templates/api/templates";
import type { TemplateField } from "@/features/templates/api/templates";
import { templateFieldSchema } from "@/features/templates/schemas/templates";

const fields: TemplateField[] = [
  { key: "title", label: "Title", type: "text", required: true, max_length: 5, pattern: "[a-z]+" },
  { key: "opt", label: "Opt", type: "text", required: false },
  { key: "secret", label: "Secret", type: "secret", required: true },
  { key: "count", label: "Count", type: "number", required: true, min: 1, max: 10 },
  { key: "optnum", label: "OptNum", type: "number", required: false, min: 0, max: 5 },
  { key: "flag", label: "Flag", type: "bool", required: false },
  { key: "mode", label: "Mode", type: "select", required: true, options: ["slow", "fast"] },
  { key: "optsel", label: "OptSel", type: "select", required: false, options: ["a", "b"] },
  { key: "badpat", label: "Bad", type: "text", required: false, pattern: "([a-z" },
];

const probeValues: string[] = [
  "",
  "   ",
  "abc",
  "ABC",
  "abcdef",
  "héllo",
  "é",
  "😀",
  "0",
  "7",
  "-3",
  "+5",
  "007",
  "1.5",
  " 7 ",
  "9999999999999999999999",
  "true",
  "True",
  " TRUE ",
  "false",
  "maybe",
  "slow",
  " slow ",
  "turbo",
];

// [fieldIndex, valueIndex, recordedOutcome] from the old checkTemplateValue.
const recorded: Array<[number, number, string | null]> = [
  [0,0,"This field is required."], [0,1,"This field is required."], [0,2,null], [0,3,"Does not match the required format."], [0,4,"Must be at most 5 characters."],
  [0,5,"Does not match the required format."], [0,6,"Does not match the required format."], [0,7,"Does not match the required format."], [0,8,"Does not match the required format."], [0,9,"Does not match the required format."],
  [0,10,"Does not match the required format."], [0,11,"Does not match the required format."], [0,12,"Does not match the required format."], [0,13,"Does not match the required format."], [0,2,null],
  [0,14,"Does not match the required format."], [0,15,"Must be at most 5 characters."], [0,16,null], [0,17,"Does not match the required format."], [0,18,"Must be at most 5 characters."],
  [0,19,null], [0,20,null], [0,21,null], [0,22,"Must be at most 5 characters."], [0,23,null],
  [1,0,null], [1,1,null], [1,2,null], [1,3,null], [1,4,null],
  [1,5,null], [1,6,null], [1,7,null], [1,8,null], [1,9,null],
  [1,10,null], [1,11,null], [1,12,null], [1,13,null], [1,2,null],
  [1,14,null], [1,15,null], [1,16,null], [1,17,null], [1,18,null],
  [1,19,null], [1,20,null], [1,21,null], [1,22,null], [1,23,null],
  [2,0,"This field is required."], [2,1,"This field is required."], [2,2,null], [2,3,null], [2,4,null],
  [2,5,null], [2,6,null], [2,7,null], [2,8,null], [2,9,null],
  [2,10,null], [2,11,null], [2,12,null], [2,13,null], [2,2,null],
  [2,14,null], [2,15,null], [2,16,null], [2,17,null], [2,18,null],
  [2,19,null], [2,20,null], [2,21,null], [2,22,null], [2,23,null],
  [3,0,"This field is required."], [3,1,"This field is required."], [3,2,"Must be a whole number."], [3,3,"Must be a whole number."], [3,4,"Must be a whole number."],
  [3,5,"Must be a whole number."], [3,6,"Must be a whole number."], [3,7,"Must be a whole number."], [3,8,"Must be at least 1."], [3,9,null],
  [3,10,"Must be at least 1."], [3,11,"Must be a whole number."], [3,12,null], [3,13,"Must be a whole number."], [3,2,"Must be a whole number."],
  [3,14,null], [3,15,"Must be at most 10."], [3,16,"Must be a whole number."], [3,17,"Must be a whole number."], [3,18,"Must be a whole number."],
  [3,19,"Must be a whole number."], [3,20,"Must be a whole number."], [3,21,"Must be a whole number."], [3,22,"Must be a whole number."], [3,23,"Must be a whole number."],
  [4,0,null], [4,1,null], [4,2,"Must be a whole number."], [4,3,"Must be a whole number."], [4,4,"Must be a whole number."],
  [4,5,"Must be a whole number."], [4,6,"Must be a whole number."], [4,7,"Must be a whole number."], [4,8,null], [4,9,"Must be at most 5."],
  [4,10,"Must be at least 0."], [4,11,"Must be a whole number."], [4,12,"Must be at most 5."], [4,13,"Must be a whole number."], [4,2,"Must be a whole number."],
  [4,14,"Must be at most 5."], [4,15,"Must be at most 5."], [4,16,"Must be a whole number."], [4,17,"Must be a whole number."], [4,18,"Must be a whole number."],
  [4,19,"Must be a whole number."], [4,20,"Must be a whole number."], [4,21,"Must be a whole number."], [4,22,"Must be a whole number."], [4,23,"Must be a whole number."],
  [5,0,"Must be true or false."], [5,1,"Must be true or false."], [5,2,"Must be true or false."], [5,3,"Must be true or false."], [5,4,"Must be true or false."],
  [5,5,"Must be true or false."], [5,6,"Must be true or false."], [5,7,"Must be true or false."], [5,8,"Must be true or false."], [5,9,"Must be true or false."],
  [5,10,"Must be true or false."], [5,11,"Must be true or false."], [5,12,"Must be true or false."], [5,13,"Must be true or false."], [5,2,"Must be true or false."],
  [5,14,"Must be true or false."], [5,15,"Must be true or false."], [5,16,null], [5,17,"Must be true or false."], [5,18,"Must be true or false."],
  [5,19,null], [5,20,"Must be true or false."], [5,21,"Must be true or false."], [5,22,"Must be true or false."], [5,23,"Must be true or false."],
  [6,0,"This field is required."], [6,1,"This field is required."], [6,2,"Must be one of: slow, fast."], [6,3,"Must be one of: slow, fast."], [6,4,"Must be one of: slow, fast."],
  [6,5,"Must be one of: slow, fast."], [6,6,"Must be one of: slow, fast."], [6,7,"Must be one of: slow, fast."], [6,8,"Must be one of: slow, fast."], [6,9,"Must be one of: slow, fast."],
  [6,10,"Must be one of: slow, fast."], [6,11,"Must be one of: slow, fast."], [6,12,"Must be one of: slow, fast."], [6,13,"Must be one of: slow, fast."], [6,2,"Must be one of: slow, fast."],
  [6,14,"Must be one of: slow, fast."], [6,15,"Must be one of: slow, fast."], [6,16,"Must be one of: slow, fast."], [6,17,"Must be one of: slow, fast."], [6,18,"Must be one of: slow, fast."],
  [6,19,"Must be one of: slow, fast."], [6,20,"Must be one of: slow, fast."], [6,21,null], [6,22,null], [6,23,"Must be one of: slow, fast."],
  [7,0,"Must be one of: a, b."], [7,1,"Must be one of: a, b."], [7,2,"Must be one of: a, b."], [7,3,"Must be one of: a, b."], [7,4,"Must be one of: a, b."],
  [7,5,"Must be one of: a, b."], [7,6,"Must be one of: a, b."], [7,7,"Must be one of: a, b."], [7,8,"Must be one of: a, b."], [7,9,"Must be one of: a, b."],
  [7,10,"Must be one of: a, b."], [7,11,"Must be one of: a, b."], [7,12,"Must be one of: a, b."], [7,13,"Must be one of: a, b."], [7,2,"Must be one of: a, b."],
  [7,14,"Must be one of: a, b."], [7,15,"Must be one of: a, b."], [7,16,"Must be one of: a, b."], [7,17,"Must be one of: a, b."], [7,18,"Must be one of: a, b."],
  [7,19,"Must be one of: a, b."], [7,20,"Must be one of: a, b."], [7,21,"Must be one of: a, b."], [7,22,"Must be one of: a, b."], [7,23,"Must be one of: a, b."],
  [8,0,null], [8,1,null], [8,2,null], [8,3,null], [8,4,null],
  [8,5,null], [8,6,null], [8,7,null], [8,8,null], [8,9,null],
  [8,10,null], [8,11,null], [8,12,null], [8,13,null], [8,2,null],
  [8,14,null], [8,15,null], [8,16,null], [8,17,null], [8,18,null],
  [8,19,null], [8,20,null], [8,21,null], [8,22,null], [8,23,null],
];

describe("template field schemas match recorded outcomes", () => {
  it("reproduces every recorded outcome and message", () => {
    expect(recorded.length).toBe(225);
    for (const [fi, vi, expected] of recorded) {
      const field = fields[fi];
      const value = probeValues[vi];
      const parsed = templateFieldSchema(field).safeParse(value);
      const actual = parsed.success ? null : (parsed.error.issues[0]?.message ?? null);
      expect({ field: field.key, value, actual }).toEqual({ field: field.key, value, actual: expected });
      expect(checkTemplateValue(field, value)).toBe(expected);
    }
  });

  it("reproduces validateTemplateValues first-message-per-field", () => {
    expect(validateTemplateValues(fields, {"title": "", "count": "abc", "mode": "", "flag": "maybe"})).toEqual({"title": "This field is required.", "secret": "This field is required.", "count": "Must be a whole number.", "flag": "Must be true or false.", "mode": "This field is required.", "optsel": "Must be one of: a, b."});
  });
});
