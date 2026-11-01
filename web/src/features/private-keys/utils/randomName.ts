import { names, uniqueNamesGenerator } from "unique-names-generator";

const suffixAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789";

export function randomKeyName(): string {
  const firstName = uniqueNamesGenerator({
    dictionaries: [names],
    length: 1,
    style: "lowerCase",
  }).replace(/[^a-z]/g, "");
  const bytes = crypto.getRandomValues(new Uint8Array(12));
  const suffix = Array.from(
    bytes,
    (byte) => suffixAlphabet[byte % suffixAlphabet.length],
  ).join("");
  return `${firstName || "key"}-${suffix}`;
}
