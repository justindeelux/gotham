/**
 * Merges the lazy files-editor catalogs under `files.editor` at import time.
 * Imported only by the files-page components, so the editor messages ship in
 * the lazy files-page chunk (with the editor) instead of the eager feature
 * catalog that lands in the entry chunk. Both locales are merged once; a
 * later language switch just re-renders the existing messages.
 */
import { i18n } from "@/shared/i18n";
import editorEn from "@/features/files/locales/editorEn";
import editorVi from "@/features/files/locales/editorVi";

i18n.global.mergeLocaleMessage("en", { files: { editor: editorEn } });
i18n.global.mergeLocaleMessage("vi", { files: { editor: editorVi } });
