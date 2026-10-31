import { names, uniqueNamesGenerator } from "unique-names-generator";

const suffixAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789";

// Matches the key-manager rule: an English first name plus 12 random
// [a-z0-9] characters, lowercase.
export function generateKeyName(): string {
  const firstName = uniqueNamesGenerator({
    dictionaries: [names],
    length: 1,
    separator: "",
    style: "lowerCase",
  })
    .toLowerCase()
    .replace(/[^a-z]/g, "");

  const bytes = crypto.getRandomValues(new Uint8Array(12));
  let suffix = "";
  for (const byte of bytes) {
    suffix += suffixAlphabet[byte % suffixAlphabet.length];
  }
  return `${firstName || "key"}-${suffix}`;
}
