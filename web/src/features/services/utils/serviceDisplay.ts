export function cardMark(name: string): string {
  const first = name.trim()[0];
  return first ? first.toUpperCase() : "?";
}
