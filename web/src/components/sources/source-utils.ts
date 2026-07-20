import type { Source } from "@/types/api"

export function sourcePathParts(source: Source): string[] {
  if (source.breadcrumbs.length > 0) return source.breadcrumbs.filter(Boolean)
  if (source.heading_path.length > 0) return source.heading_path.filter(Boolean)
  if (source.source) return [source.source]
  return source.title ? [source.title] : []
}

export function sourcePath(source: Source): string {
  return sourcePathParts(source).join(" → ")
}

export function uniqueSources(sources: Source[]): Source[] {
  const sourceIDs = new Set<string>()
  const chunkIDs = new Set<string>()

  return sources.filter((source) => {
    if (sourceIDs.has(source.source_id)) return false
    if (source.chunk_id && chunkIDs.has(source.chunk_id)) return false
    sourceIDs.add(source.source_id)
    if (source.chunk_id) chunkIDs.add(source.chunk_id)
    return true
  })
}

export function safeExternalURL(value?: string): string | null {
  if (!value) return null
  try {
    const parsed = new URL(value)
    return parsed.protocol === "http:" || parsed.protocol === "https:" ? parsed.href : null
  } catch {
    return null
  }
}
