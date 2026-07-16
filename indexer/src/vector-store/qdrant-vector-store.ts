import { QdrantClient } from "@qdrant/js-client-rest";
import type {
  CollectionConfig,
  CollectionState,
  PayloadUpdate,
  PipelineIdentity,
  PointPayload,
  StoredPoint,
  VectorLookup,
  VectorPoint,
  VectorStore,
} from "./vector-store.js";

export interface QdrantVectorStoreOptions {
  url: string;
  collection: string;
  apiKey?: string;
}

export class QdrantVectorStore implements VectorStore {
  private readonly client: QdrantClient;
  private readonly collection: string;

  constructor(options: QdrantVectorStoreOptions) {
    this.collection = options.collection;
    this.client = new QdrantClient({
      url: options.url,
      ...(options.apiKey ? { apiKey: options.apiKey } : {}),
      checkCompatibility: true,
    });
  }

  async ping(): Promise<void> {
    await this.client.getCollections();
  }

  async ensureCollection(config: CollectionConfig, dryRun: boolean): Promise<CollectionState> {
    const existence = await this.client.collectionExists(this.collection);
    if (!existence.exists) {
      if (dryRun) return { exists: false, created: false, wouldCreate: true };
      const response = await this.client.api().createCollection({
        collection_name: this.collection,
        vectors: { size: config.dimension, distance: config.distance },
        metadata: { ...config.pipeline },
      });
      if (!response.ok || response.data.result !== true) {
        throw new Error(`Qdrant did not create collection ${this.collection}.`);
      }
      return { exists: false, created: true, wouldCreate: false };
    }

    const info = await this.client.getCollection(this.collection);
    const vectors = info.config.params.vectors;
    if (!isRecord(vectors) || typeof vectors.size !== "number" || typeof vectors.distance !== "string") {
      throw new Error(`Collection ${this.collection} uses named or unsupported vector configuration.`);
    }
    if (vectors.size !== config.dimension || vectors.distance.toLowerCase() !== config.distance.toLowerCase()) {
      throw new Error(
        `Collection ${this.collection} is incompatible: expected ${config.dimension}/${config.distance}, got ${vectors.size}/${vectors.distance}.`,
      );
    }
    const metadata = info.config.metadata;
    if (!isRecord(metadata)) {
      throw new Error(`Collection ${this.collection} has no embedding pipeline metadata.`);
    }
    for (const [key, expected] of Object.entries(config.pipeline)) {
      if (metadata[key] !== expected) {
        throw new Error(
          `Collection ${this.collection} pipeline metadata mismatch for ${key}: expected ${String(expected)}, got ${String(metadata[key])}. Use a new collection.`,
        );
      }
    }
    return { exists: true, created: false, wouldCreate: false };
  }

  async ensurePayloadIndexes(fields: readonly string[], dryRun: boolean): Promise<void> {
    if (dryRun) return;
    const info = await this.client.getCollection(this.collection);
    for (const field of fields) {
      if (field in info.payload_schema) continue;
      await this.client.createPayloadIndex(this.collection, {
        field_name: field,
        field_schema: "keyword",
        wait: true,
      });
    }
  }

  async getPoints(ids: string[]): Promise<StoredPoint[]> {
    if (ids.length === 0) return [];
    const points = await this.client.retrieve(this.collection, {
      ids,
      with_payload: true,
      with_vector: false,
    });
    return points.map((point) => ({
      id: String(point.id),
      payload: isRecord(point.payload) ? point.payload : {},
    }));
  }

  async findVector(lookup: VectorLookup): Promise<number[] | null> {
    const result = await this.client.scroll(this.collection, {
      limit: 1,
      with_payload: false,
      with_vector: true,
      filter: { must: lookupConditions(lookup) },
    });
    const vector = result.points[0]?.vector;
    return toDenseVector(vector);
  }

  async upsert(points: VectorPoint[]): Promise<void> {
    if (points.length === 0) return;
    await this.client.upsert(this.collection, {
      wait: true,
      points: points.map((point) => ({ id: point.id, vector: point.vector, payload: point.payload })),
    });
  }

  async updatePayload(points: PayloadUpdate[]): Promise<void> {
    for (const point of points) {
      await this.client.overwritePayload(this.collection, {
        wait: true,
        points: [point.id],
        payload: point.payload,
      });
    }
  }

  async deletePoints(ids: string[]): Promise<void> {
    if (ids.length === 0) return;
    await this.client.delete(this.collection, { wait: true, points: ids });
  }

  async listManagedPointIds(pipeline: PipelineIdentity): Promise<string[]> {
    const ids: string[] = [];
    let offset: string | number | undefined;
    do {
      const result = await this.client.scroll(this.collection, {
        limit: 256,
        ...(offset !== undefined ? { offset } : {}),
        with_payload: false,
        with_vector: false,
        filter: { must: pipelineConditions(pipeline) },
      });
      ids.push(...result.points.map((point) => String(point.id)));
      const next = result.next_page_offset;
      offset = typeof next === "string" || typeof next === "number" ? next : undefined;
    } while (offset !== undefined);
    return ids;
  }
}

function lookupConditions(lookup: VectorLookup): Array<Record<string, unknown>> {
  return [
    { key: "embedding_input_hash", match: { value: lookup.embeddingInputHash } },
    ...pipelineConditions(lookup.pipeline),
  ];
}

function pipelineConditions(pipeline: PipelineIdentity): Array<Record<string, unknown>> {
  return [
    { key: "point_namespace", match: { value: pipeline.point_namespace } },
    { key: "embedding_model", match: { value: pipeline.embedding_model } },
    {
      key: "embedding_model_revision",
      match: { value: pipeline.embedding_model_revision },
    },
    {
      key: "embedding_pipeline_version",
      match: { value: pipeline.embedding_pipeline_version },
    },
  ];
}

function toDenseVector(value: unknown): number[] | null {
  if (!Array.isArray(value) || !value.every((item) => typeof item === "number")) return null;
  return value as number[];
}

function isRecord(value: unknown): value is PointPayload {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
