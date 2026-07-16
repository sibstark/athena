import { describe, expect, it } from "vitest";
import { compareVectors, jaccard, overlap, recallAt, spearmanForCommon } from "../src/metrics.js";

describe("compatibility metrics", () => {
  it("calculates cosine and absolute differences", () => {
    const result = compareVectors([1, 0], [0.8, 0.6]);
    expect(result.cosine).toBeCloseTo(0.8);
    expect(result.max_absolute_difference).toBeCloseTo(0.6);
  });

  it("calculates retrieval overlap, Jaccard and top-1 agreement inputs", () => {
    expect(overlap(["a", "b", "c"], ["a", "c", "x"], 3)).toBeCloseTo(2 / 3);
    expect(jaccard(["a", "b"], ["a", "c"], 2)).toBeCloseTo(1 / 3);
    expect(spearmanForCommon(["a", "b", "c"], ["a", "b", "c"])).toBe(1);
  });

  it("calculates recall@K", () => {
    expect(recallAt(["page_1", "page_2"], ["page_2"], 1)).toBe(0);
    expect(recallAt(["page_1", "page_2"], ["page_2"], 5)).toBe(1);
    expect(recallAt([], [], 5)).toBeNull();
  });
});
