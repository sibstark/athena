import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import type { CompatibilityReport } from "./compatibility.js";

export async function writeCompatibilityReports(jsonPath: string, report: CompatibilityReport): Promise<{ json: string; markdown: string }> {
  const markdownPath = jsonPath.replace(/\.json$/u, "") + ".md";
  await mkdir(dirname(jsonPath), { recursive: true });
  await Promise.all([
    writeFile(jsonPath, `${JSON.stringify(report, null, 2)}\n`, "utf8"),
    writeFile(markdownPath, markdown(report), "utf8"),
  ]);
  return { json: jsonPath, markdown: markdownPath };
}

function markdown(report: CompatibilityReport): string {
  const document = report.document_vector_comparison as Record<string, unknown>;
  const cosine = (document.cosine ?? {}) as Record<string, unknown>;
  const retrieval = report.retrieval_comparison as Record<string, unknown>;
  return `# TEI compatibility report

- Verdict: **${report.verdict}**
- Read-only: ${report.read_only}
- Collection unchanged: ${report.collection_unchanged}
- Model: ${String(report.tei.model_id)}
- Revision: ${String(report.tei.model_revision)}
- TEI: ${String(report.tei.version)} (${String(report.tei.backend)})
- Image: ${String(report.tei.image)}${report.tei.image_digest ? `@${String(report.tei.image_digest)}` : " (digest unknown)"}
- Qdrant points: ${String(report.qdrant.points_count)}
- Document vectors compared: ${String(document.compared ?? 0)}
- Paired cosine mean / p05 / min: ${fmt(cosine.mean)} / ${fmt(cosine.p05)} / ${fmt(cosine.minimum)}
- Query top-1 agreement: ${fmt(retrieval.top1_agreement)}
- Query top-5 overlap: ${fmt(retrieval.average_top5_overlap)}
- Query top-10 overlap: ${fmt(retrieval.average_top10_overlap)}

## Recommendation

${report.recommendation}

## Warnings

${list(report.warnings)}

## Errors

${list(report.errors)}
`;
}

function fmt(value: unknown): string {
  return typeof value === "number" ? value.toFixed(6) : "n/a";
}

function list(values: string[]): string {
  return values.length ? values.map((value) => `- ${value}`).join("\n") : "None.";
}
