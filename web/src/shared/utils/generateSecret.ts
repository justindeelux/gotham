export type SecretCharset = "alphanumeric" | "hex" | "base64";

const ALPHABETS: Record<Exclude<SecretCharset, "base64">, string> = {
  alphanumeric: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789",
  hex: "0123456789abcdef",
};

// length counts characters for alphanumeric and hex, and random bytes for
// base64, which is standard padded base64 of those bytes.
export function generateSecret(
  length = 24,
  charset: SecretCharset = "alphanumeric",
  prefix = "",
): string {
  if (charset === "base64") {
    const bytes = crypto.getRandomValues(new Uint8Array(length));
    let binary = "";
    for (const byte of bytes) {
      binary += String.fromCharCode(byte);
    }
    return prefix + btoa(binary);
  }
  const alphabet = ALPHABETS[charset];
  // Bytes at or above this cutoff are rejected so every character is equally likely.
  const rejectionLimit = Math.floor(256 / alphabet.length) * alphabet.length;
  let secret = "";
  while (secret.length < length) {
    const bytes = crypto.getRandomValues(new Uint8Array(length));
    for (const byte of bytes) {
      if (byte >= rejectionLimit) {
        continue;
      }
      secret += alphabet[byte % alphabet.length];
      if (secret.length === length) {
        break;
      }
    }
  }
  return prefix + secret;
}
