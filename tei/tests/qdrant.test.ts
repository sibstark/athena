import { describe, expect, it, vi } from "vitest";
import { ReadOnlyQdrantStore, type ReadOnlyQdrantApi } from "../src/qdrant/read-only-qdrant-store.js";

describe("ReadOnlyQdrantStore", () => {
  it("exposes only read operations and parses collection state", async () => {
    const client: ReadOnlyQdrantApi = {
      getCollections: vi.fn(async () => ({})),
      getCollection: vi.fn(async () => ({
        status: "green",
        points_count: 2383,
        config: { params: { vectors: { size: 384, distance: "Cosine" } }, metadata: { embedding_model: "Xenova/multilingual-e5-small" } },
        payload_schema: {},
      })),
      retrieve: vi.fn(async () => []),
      search: vi.fn(async () => []),
    };
    const store = new ReadOnlyQdrantStore({ url: "http://qdrant", collection: "test", client });
    expect(await store.snapshot()).toMatchObject({ pointsCount: 2383, dimension: 384, distance: "Cosine" });
    expect("upsert" in store).toBe(false);
    expect("deletePoints" in store).toBe(false);
    expect("createCollection" in store).toBe(false);
  });
});
