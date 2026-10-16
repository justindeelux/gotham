/**
 * downloadText saves text as a file through a Blob and an object URL. It backs
 * the one-time recovery-code download (JUS-196): the codes are shown once, so
 * the user needs a way to keep them without a server round trip. The object URL
 * is released on the next task, after the click has started the download.
 */
export function downloadText(filename: string, contents: string): void {
  const blob = new Blob([contents], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
