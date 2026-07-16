export type PointPayload = Record<string, unknown>;

export interface PipelineIdentity {
  embedding_model: string;
  embedding_model_revision: string;
  embedding_dimension: number;
  embedding_dtype: string;
  embedding_pooling: "mean";
  embedding_normalized: true;
  document_prefix: "passage:";
  embedding_pipeline_version: string;
  point_namespace: string;
}

export interface CollectionConfig {
  name: string;
  dimension: number;
  distance: "Cosine";
  pipeline: PipelineIdentity;
}

export interface CollectionState {
  exists: boolean;
  created: boolean;
  wouldCreate: boolean;
}

export interface StoredPoint {
  id: string;
  payload: PointPayload;
  vector?: number[];
}

export interface VectorPoint extends StoredPoint {
  vector: number[];
}

export interface PayloadUpdate {
  id: string;
  payload: PointPayload;
}

export interface VectorLookup {
  embeddingInputHash: string;
  pipeline: PipelineIdentity;
}

export interface VectorStore {
  ping(): Promise<void>;
  ensureCollection(config: CollectionConfig, dryRun: boolean): Promise<CollectionState>;
  ensurePayloadIndexes(fields: readonly string[], dryRun: boolean): Promise<void>;
  getPoints(ids: string[]): Promise<StoredPoint[]>;
  findVector(lookup: VectorLookup): Promise<number[] | null>;
  upsert(points: VectorPoint[]): Promise<void>;
  updatePayload(points: PayloadUpdate[]): Promise<void>;
  deletePoints(ids: string[]): Promise<void>;
  listManagedPointIds(pipeline: PipelineIdentity): Promise<string[]>;
}
