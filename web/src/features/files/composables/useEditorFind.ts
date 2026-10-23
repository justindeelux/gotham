import { computed, onScopeDispose, ref, watch } from "vue";

import type { GotoLineError } from "@/features/files/editor/goto";
import { validateSearchPattern } from "@/features/files/editor/searchPattern";
import type { SearchPatternError } from "@/features/files/editor/searchPattern";
import type {
  EditorSearchOptions,
  EditorSearchStatus,
  SearchableEditor,
} from "@/features/files/editor/search";

export interface UseEditorFindOptions {
  session: () => SearchableEditor | null;
  tabId: () => string;
  canReplace: () => boolean;
}

interface SavedFindState extends EditorSearchOptions {
  replacement: string;
  replaceExpanded: boolean;
}

const QUERY_DEBOUNCE_MS = 180;

const STATUS_DEBOUNCE_MS = 220;

const EMPTY_STATUS: EditorSearchStatus = { total: 0, current: 0, capped: false };

export function useEditorFind(options: UseEditorFindOptions) {
  const findOpen = ref(false);
  const replaceExpanded = ref(false);
  const focusReplace = ref(false);
  const gotoOpen = ref(false);
  const query = ref("");
  const replacement = ref("");
  const caseSensitive = ref(false);
  const wholeWord = ref(false);
  const regexp = ref(false);
  const total = ref(0);
  const current = ref(0);
  const capped = ref(false);
  const gotoCurrent = ref(1);
  const gotoTotal = ref(1);
  const saved = new Map<string, SavedFindState>();
  let queryTimer: ReturnType<typeof setTimeout> | null = null;
  let statusTimer: ReturnType<typeof setTimeout> | null = null;

  const canReplace = computed<boolean>(() => options.canReplace());

  const patternError = computed<SearchPatternError | null>(() => {
    if (!regexp.value || query.value.length === 0) {
      return null;
    }
    return validateSearchPattern(query.value);
  });

  function clearQueryTimer(): void {
    if (queryTimer !== null) {
      clearTimeout(queryTimer);
      queryTimer = null;
    }
  }

  function clearStatusTimer(): void {
    if (statusTimer !== null) {
      clearTimeout(statusTimer);
      statusTimer = null;
    }
  }

  function currentOptions(): EditorSearchOptions {
    return {
      query: query.value,
      caseSensitive: caseSensitive.value,
      wholeWord: wholeWord.value,
      regexp: regexp.value,
    };
  }

  function publish(status: EditorSearchStatus): void {
    total.value = status.total;
    current.value = status.current;
    capped.value = status.capped;
  }

  function applySearch(): void {
    const editor = options.session();
    if (!editor) {
      return;
    }
    if (patternError.value) {
      editor.search.clear();
      publish(EMPTY_STATUS);
      return;
    }
    publish(editor.search.setOptions(currentOptions(), replacement.value));
  }

  function applyNow(): void {
    clearQueryTimer();
    clearStatusTimer();
    applySearch();
  }

  function loadState(tabId: string): void {
    const stored = saved.get(tabId);
    query.value = stored?.query ?? "";
    replacement.value = stored?.replacement ?? "";
    caseSensitive.value = stored?.caseSensitive ?? false;
    wholeWord.value = stored?.wholeWord ?? false;
    regexp.value = stored?.regexp ?? false;
    replaceExpanded.value = stored?.replaceExpanded ?? false;
  }

  watch(options.tabId, (next, previous) => {
    clearQueryTimer();
    clearStatusTimer();
    if (previous) {
      saved.set(previous, {
        ...currentOptions(),
        replacement: replacement.value,
        replaceExpanded: replaceExpanded.value,
      });
    }
    loadState(next);
    if (findOpen.value) {
      applySearch();
    }
    if (gotoOpen.value) {
      refreshGotoInfo();
    }
  });

  function openFind(expand: boolean): void {
    if (gotoOpen.value) {
      closeGotoLine(false);
    }
    if (expand) {
      replaceExpanded.value = true;
    }
    focusReplace.value = expand;
    findOpen.value = true;
    applyNow();
  }

  function closeFind(focus = true): void {
    if (!findOpen.value) {
      return;
    }
    clearQueryTimer();
    clearStatusTimer();
    findOpen.value = false;
    options.session()?.search.clear();
    publish(EMPTY_STATUS);
    if (focus) {
      options.session()?.focus();
    }
  }

  function toggleReplace(): void {
    replaceExpanded.value = !replaceExpanded.value;
  }

  function setQuery(value: string): void {
    query.value = value;
    clearQueryTimer();
    if (patternError.value) {
      applyNow();
      return;
    }
    queryTimer = setTimeout(() => {
      queryTimer = null;
      applySearch();
    }, QUERY_DEBOUNCE_MS);
  }

  function setReplacement(value: string): void {
    replacement.value = value;
  }

  function toggleOption(option: "caseSensitive" | "wholeWord" | "regexp"): void {
    if (option === "caseSensitive") {
      caseSensitive.value = !caseSensitive.value;
    } else if (option === "wholeWord") {
      wholeWord.value = !wholeWord.value;
    } else {
      regexp.value = !regexp.value;
    }
    applyNow();
  }

  function syncPending(): void {
    if (queryTimer !== null || statusTimer !== null) {
      applyNow();
    }
  }

  function next(): void {
    syncPending();
    const editor = options.session();
    if (editor && !patternError.value) {
      publish(editor.search.next());
    }
  }

  function previous(): void {
    syncPending();
    const editor = options.session();
    if (editor && !patternError.value) {
      publish(editor.search.previous());
    }
  }

  function replaceMatch(): void {
    syncPending();
    const editor = options.session();
    if (editor && canReplace.value && !patternError.value) {
      publish(editor.search.replace(replacement.value));
    }
  }

  function replaceEveryMatch(): void {
    syncPending();
    const editor = options.session();
    if (editor && canReplace.value && !patternError.value) {
      publish(editor.search.replaceAll(replacement.value));
    }
  }

  function refreshGotoInfo(): void {
    const info = options.session()?.lineInfo();
    gotoCurrent.value = info?.line ?? 1;
    gotoTotal.value = info?.total ?? 1;
  }

  function openGotoLine(): void {
    if (findOpen.value) {
      closeFind(false);
    }
    refreshGotoInfo();
    gotoOpen.value = true;
  }

  function closeGotoLine(focus = true): void {
    if (!gotoOpen.value) {
      return;
    }
    gotoOpen.value = false;
    if (focus) {
      options.session()?.focus();
    }
  }

  function jumpToLine(line: number, column: number): GotoLineError | null {
    const editor = options.session();
    if (!editor) {
      return "line";
    }
    const failure = editor.gotoLine(line, column);
    if (failure) {
      return failure;
    }
    gotoOpen.value = false;
    editor.focus();
    return null;
  }

  function refresh(): void {
    if (findOpen.value) {
      applySearch();
    }
    if (gotoOpen.value) {
      refreshGotoInfo();
    }
  }

  function queueStatus(): void {
    if (!findOpen.value) {
      return;
    }
    clearStatusTimer();
    statusTimer = setTimeout(() => {
      statusTimer = null;
      applySearch();
    }, STATUS_DEBOUNCE_MS);
  }

  onScopeDispose(() => {
    clearQueryTimer();
    clearStatusTimer();
  });

  return {
    findOpen,
    replaceExpanded,
    focusReplace,
    gotoOpen,
    query,
    replacement,
    caseSensitive,
    wholeWord,
    regexp,
    total,
    current,
    capped,
    gotoCurrent,
    gotoTotal,
    canReplace,
    patternError,
    openFind,
    closeFind,
    toggleReplace,
    setQuery,
    setReplacement,
    toggleCase: () => toggleOption("caseSensitive"),
    toggleWord: () => toggleOption("wholeWord"),
    toggleRegex: () => toggleOption("regexp"),
    next,
    previous,
    replaceMatch,
    replaceEveryMatch,
    openGotoLine,
    closeGotoLine,
    jumpToLine,
    refresh,
    queueStatus,
  };
}
