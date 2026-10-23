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
