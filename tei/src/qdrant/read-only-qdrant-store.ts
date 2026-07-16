import { QdrantClient } from "@qdrant/js-client-rest";
import type { CollectionSnapshot, SearchHit, StoredPoint } from "../contracts.js";
import { uuidV5 } from "../shared/deterministic-uuid.js";

export interface ReadOnlyQdrantApi {
  getCollections(): Promise<unknown>;
  getCollection(name: string): Promise<unknown>;
  retrieve(name: string, request: Record<string, unknown>): Promise<unknown>;
  search(name: string, request: Record<string, unknown>): Promise<unknown>;
}

export class ReadOnlyQdrantStore {
  private readonly client: ReadOnlyQdrantApi;
  readonly collection: string;

  constructor(options: { url: string; collection: string; apiKey?: string; client?: ReadOnlyQdrantApi }) {
    this.collection = options.collection;
    this.client = options.client ?? new QdrantClient({
      url: options.url,
      ...(options.apiKey ? { apiKey: options.apiKey } : {}),
      checkCompatibility: true,
    }) as unknown as ReadOnlyQdrantApi;
  }

  async ping(): Promise<void> {
    await this.client.getCollections();
  }

  async snapshot(): Promise<CollectionSnapshot> {
    const raw = await this.client.getCollection(this.collection);
    if (!isRecord(raw)) throw new Error("Qdrant returned invalid collection information.");
    const config = record(raw.config, "collection config");
    const params = record(config.params, "collection params");
    const vectors = record(params.vectors, "vector config");
    if (typeof vectors.size !== "number" || typeof vectors.distance !== "string") {
      throw new Error("Named or unsupported Qdrant vector configuration.");
    }
    return {
      name: this.collection,
      status: String(raw.status ?? "unknown"),
      pointsCount: numberValue(raw.points_count),
      dimension: vectors.size,
      distance: vectors.distance,
      metadata: isRecord(config.metadata) ? config.metadata : {},
      payloadSchema: isRecord(raw.payload_schema) ? raw.payload_schema : {},
    };
  }

  async getPointsByChunkIds(chunkIds: readonly string[]): Promise<Map<string, StoredPoint>> {
    if (chunkIds.length === 0) return new Map();
    const response = await this.client.retrieve(this.collection, {
      ids: chunkIds.map((id) => uuidV5(id)),
      with_payload: true,
      with_vector: true,
    });
    if (!Array.isArray(response)) throw new Error("Qdrant retrieve returned invalid data.");
    const points = new Map<string, StoredPoint>();
    for (const item of response) {
      if (!isRecord(item)) continue;
      const payload = isRecord(item.payload) ? item.payload : {};
      const chunkId = typeof payload.chunk_id === "string" ? payload.chunk_id : undefined;
      const vector = denseVector(item.vector);
      if (!chunkId || !vector) continue;
      points.set(chunkId, { id: String(item.id), payload, vector });
    }
    return points;
  }

  async search(vector: number[], limit: number): Promise<SearchHit[]> {
    const response = await this.client.search(this.collection, {
      vector,
      limit,
      with_payload: true,
      with_vector: false,
    });
    if (!Array.isArray(response)) throw new Error("Qdrant search returned invalid data.");
    return response.flatMap((item): SearchHit[] => {
      if (!isRecord(item) || typeof item.score !== "number") return [];
      return [{
        id: String(item.id),
        score: item.score,
        payload: isRecord(item.payload) ? item.payload : {},
      }];
    });
  }
}

function denseVector(value: unknown): number[] | null {
  if (Array.isArray(value) && value.every((item) => typeof item === "number")) return value as number[];
  return null;
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (!isRecord(value)) throw new Error(`Qdrant returned invalid ${name}.`);
  return value;
}

function numberValue(value: unknown): number {
  return typeof value === "number" ? value : 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
