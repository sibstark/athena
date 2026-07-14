import { createHash } from "node:crypto";
import { dirname, resolve, sep } from "node:path";

export function sanitizeSegment(value: string, fallback = "document"): string {
  const normalized = value
    .normalize("NFKC")
    .toLocaleLowerCase("ru")
    .replace(/[<>:"/\\|?*\u0000-\u001F]/g, " ")
    .replace(/[^\p{L}\p{N}._ -]+/gu, " ")
    .trim()
    .replace(/[ .]+$/g, "")
    .replace(/[\s_]+/g, "-")
    .replace(/-+/g, "-")
    .slice(0, 80)
    .replace(/[ .-]+$/g, "");
  return normalized || fallback;
}

export function sanitizeFileName(value: string, fallback = "asset"): string {
  const decoded = safeDecodeURIComponent(value);
  const cleaned = decoded
    .normalize("NFKC")
    .replace(/[<>:"/\\|?*\u0000-\u001F]/g, "-")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^[-. ]+|[-. ]+$/g, "")
    .slice(0, 120);
  return cleaned || fallback;
}

export function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function shortHash(value: string): string {
  return createHash("sha256").update(value).digest("hex").slice(0, 12);
}

export function toPosixPath(value: string): string {
  return value.split(sep).join("/");
}

export function assertPathWithin(root: string, candidate: string): void {
  const resolvedRoot = resolve(root);
  const resolvedCandidate = resolve(candidate);
  if (
    resolvedCandidate !== resolvedRoot &&
    !resolvedCandidate.startsWith(`${resolvedRoot}${sep}`)
  ) {
    throw new Error(`Refusing to write outside export directory: ${candidate}`);
  }
}

export function relativePosix(fromFile: string, toFile: string): string {
  const fromDirectory = dirname(fromFile);
  const relative = toPosixPath(resolveRelative(fromDirectory, toFile));
  return relative.startsWith(".") ? relative : `./${relative}`;
}

function resolveRelative(from: string, to: string): string {
  const fromParts = resolve(from).split(sep);
  const toParts = resolve(to).split(sep);
  let common = 0;
  while (fromParts[common] === toParts[common] && common < fromParts.length) {
    common += 1;
  }
  return [
    ...Array.from({ length: fromParts.length - common }, () => ".."),
    ...toParts.slice(common),
  ].join(sep) || ".";
}

export async function mapLimit<T, R>(
  values: readonly T[],
  limit: number,
  worker: (value: T, index: number) => Promise<R>,
): Promise<R[]> {
  if (!Number.isInteger(limit) || limit < 1) throw new Error("limit must be positive");
  const results = new Array<R>(values.length);
  let nextIndex = 0;

  async function run(): Promise<void> {
    while (true) {
      const index = nextIndex;
      nextIndex += 1;
      if (index >= values.length) return;
      const value = values[index];
      if (value === undefined) return;
      results[index] = await worker(value, index);
    }
  }

  const workers = Array.from({ length: Math.min(limit, values.length) }, () => run());
  await Promise.all(workers);
  return results;
}

export function yamlString(value: string): string {
  return JSON.stringify(value);
}
