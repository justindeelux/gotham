/** cardMark derives the card avatar from the service name. */
export function cardMark(name: string): string {
  const first = name.trim()[0];
  return first ? first.toUpperCase() : "?";
}
