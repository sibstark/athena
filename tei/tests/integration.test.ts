import { describe, expect, it } from "vitest";
import { runtimeConfig } from "../src/config.js";
import { E5_MODEL_CONTRACT } from "../src/model-contract.js";
import { TeiE5EmbeddingProvider } from "../src/providers/tei-e5-provider.js";
import { ReadOnlyQdrantStore } from "../src/qdrant/read-only-qdrant-store.js";

const enabled = process.env.RUN_TEI_INTEGRATION === "1";

describe.skipIf(!enabled)("real TEI and Qdrant", () => {
  it("embeds a query and reads the existing collection without writes", async () => {
    const config = runtimeConfig();
    const tei = new TeiE5EmbeddingProvider({ baseUrl: config.teiUrl, timeoutMs: 120_000 });
    const store = new ReadOnlyQdrantStore({ url: config.qdrantUrl, collection: config.collection, ...(config.qdrantApiKey ? { apiKey: config.qdrantApiKey } : {}) });
    const before = await store.snapshot();
    const [vector] = await tei.embedQueries(["Проверка совместимости"]);
    expect(vector).toHaveLength(E5_MODEL_CONTRACT.dimension);
    await store.search(vector!, 1);
    expect(await store.snapshot()).toEqual(before);
  }, 180_000);
});
