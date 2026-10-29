const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
const ALPHABET_LENGTH = ALPHABET.length;
// Bytes at or above this cutoff are rejected so every character is equally likely.
const REJECTION_LIMIT = Math.floor(256 / ALPHABET_LENGTH) * ALPHABET_LENGTH;

export function generateSecret(length = 24): string {
  let secret = "";
  while (secret.length < length) {
    const bytes = crypto.getRandomValues(new Uint8Array(length));
    for (const byte of bytes) {
      if (byte >= REJECTION_LIMIT) {
        continue;
      }
      secret += ALPHABET[byte % ALPHABET_LENGTH];
      if (secret.length === length) {
        break;
      }
    }
  }
  return secret;
}
