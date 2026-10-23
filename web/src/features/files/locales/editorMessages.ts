import { i18n } from "@/shared/i18n";
import editorEn from "@/features/files/locales/editorEn";
import editorVi from "@/features/files/locales/editorVi";

i18n.global.mergeLocaleMessage("en", { files: { editor: editorEn } });
i18n.global.mergeLocaleMessage("vi", { files: { editor: editorVi } });
