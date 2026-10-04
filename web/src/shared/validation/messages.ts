/**
 * Message catalog convention (phase 1 is message-preserving: zero
 * user-visible diff, see docs/library-audit.md "Validation migration
 * inventory").
 *
 * A feature declares its strings as a plain object of rule-keyed messages
 * next to its schemas, and seeds every schema message from it:
 *
 *   // features/servers/schemas/connection.ts
 *   export const connectionMessages = {
 *     nameRequired: "Server name is required",
 *     portRange: "Port must be between 1 and 65535",
 *   } as const;
 *   export const portSchema = z.number().int().min(1, connectionMessages.portRange)
 *     .max(65535, connectionMessages.portRange);
 *
 * Rules: keep EXACT existing strings (snapshot them in unit tests before the
 * swap); one catalog per feature schemas module; shared/validation holds no
 * user-facing strings except primitives.ts fallbacks. Phase 2 rewording is a
 * separate PR with mockup comparison per docs/design/.
 */

export type MessageCatalog = Record<string, string>;
