import { describe, expect, it, vi } from "vitest";
import { ReadOnlyQdrantStore, type ReadOnlyQdrantApi } from "../src/qdrant/read-only-qdrant-store.js";
import { E5_MODEL_CONTRACT } from "../src/model-contract.js";

describe("ReadOnlyQdrantStore", () => {
  it("exposes only read operations and parses collection state", async () => {
    const client: ReadOnlyQdrantApi = {
      getCollections: vi.fn(async () => ({})),
      getCollection: vi.fn(async () => ({
        status: "green",
        points_count: 2383,
        config: { params: { vectors: { size: E5_MODEL_CONTRACT.dimension, distance: "Cosine" } }, metadata: { embedding_model: E5_MODEL_CONTRACT.modelId } },
        payload_schema: {},
      })),
      retrieve: vi.fn(async () => []),
      search: vi.fn(async () => []),
    };
    const store = new ReadOnlyQdrantStore({ url: "http://qdrant", collection: "test", client });
    expect(await store.snapshot()).toMatchObject({ pointsCount: 2383, dimension: E5_MODEL_CONTRACT.dimension, distance: "Cosine" });
    expect("upsert" in store).toBe(false);
    expect("deletePoints" in store).toBe(false);
    expect("createCollection" in store).toBe(false);
  });
});
