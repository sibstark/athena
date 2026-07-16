import { l2Norm } from "./providers/embedding-provider.js";

export interface VectorMetrics {
  dimension: number;
  left_norm: number;
  right_norm: number;
  cosine: number;
  dot_product: number;
  max_absolute_difference: number;
  mean_absolute_difference: number;
}

export function compareVectors(left: readonly number[], right: readonly number[]): VectorMetrics {
  if (left.length !== right.length) throw new Error(`Vector dimensions differ: ${left.length} and ${right.length}.`);
  let dot = 0;
  let maxDifference = 0;
  let totalDifference = 0;
  for (let index = 0; index < left.length; index += 1) {
    const a = left[index]!;
    const b = right[index]!;
    dot += a * b;
    const difference = Math.abs(a - b);
    maxDifference = Math.max(maxDifference, difference);
    totalDifference += difference;
  }
  const leftNorm = l2Norm(left);
  const rightNorm = l2Norm(right);
  return {
    dimension: left.length,
    left_norm: leftNorm,
    right_norm: rightNorm,
    cosine: dot / (leftNorm * rightNorm),
    dot_product: dot,
    max_absolute_difference: maxDifference,
    mean_absolute_difference: left.length === 0 ? 0 : totalDifference / left.length,
  };
}

export function distribution(values: readonly number[]): { mean: number; median: number; p05: number; minimum: number; maximum: number } {
  if (values.length === 0) return { mean: 0, median: 0, p05: 0, minimum: 0, maximum: 0 };
  const sorted = [...values].sort((a, b) => a - b);
  return {
    mean: values.reduce((sum, value) => sum + value, 0) / values.length,
    median: percentile(sorted, 0.5),
    p05: percentile(sorted, 0.05),
    minimum: sorted[0]!,
    maximum: sorted.at(-1)!,
  };
}

export function overlap(left: readonly string[], right: readonly string[], k: number): number {
  const leftSet = new Set(left.slice(0, k));
  const rightSet = new Set(right.slice(0, k));
  let common = 0;
  for (const id of leftSet) if (rightSet.has(id)) common += 1;
  const denominator = Math.min(k, Math.max(leftSet.size, rightSet.size));
  return denominator === 0 ? 1 : common / denominator;
}

export function jaccard(left: readonly string[], right: readonly string[], k: number): number {
  const a = new Set(left.slice(0, k));
  const b = new Set(right.slice(0, k));
  const union = new Set([...a, ...b]);
  let common = 0;
  for (const id of a) if (b.has(id)) common += 1;
  return union.size === 0 ? 1 : common / union.size;
}

export function recallAt(ids: readonly string[], expected: readonly string[], k: number): number | null {
  if (expected.length === 0) return null;
  const found = new Set(ids.slice(0, k));
  return expected.filter((id) => found.has(id)).length / expected.length;
}

export function spearmanForCommon(left: readonly string[], right: readonly string[]): number | null {
  const rightRank = new Map(right.map((id, index) => [id, index + 1]));
  const pairs = left.flatMap((id, index): Array<[number, number]> => {
    const rank = rightRank.get(id);
    return rank === undefined ? [] : [[index + 1, rank]];
  });
  if (pairs.length < 2) return null;
  const n = pairs.length;
  const sum = pairs.reduce((total, [a, b]) => total + (a - b) ** 2, 0);
  return 1 - (6 * sum) / (n * (n * n - 1));
}

function percentile(sorted: readonly number[], fraction: number): number {
  const index = Math.max(0, Math.ceil(fraction * sorted.length) - 1);
  return sorted[index]!;
}
