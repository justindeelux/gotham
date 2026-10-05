/**
 * submitOnEnter runs a dialog submit from an Enter keydown.
 *
 * Keyup listeners auto-submit from the keystroke that opened the dialog: a
 * keyboard user activates the opener button on keydown, focus moves into the
 * dialog, and the same key's keyup then submits a prefilled form untouched.
 * Keydown cannot fire from the opening keystroke (it already happened), so
 * only an explicit Enter inside the form submits. IME compositions and key
 * repeats never submit.
 */
export function submitOnEnter(
  event: KeyboardEvent,
  submit: () => void | Promise<void>,
): void {
  if (event.isComposing || event.repeat) {
    return;
  }
  event.preventDefault();
  void submit();
}
