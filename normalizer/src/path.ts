export function toPosixPath(value: string): string {
  return value.replaceAll("\\", "/");
}
