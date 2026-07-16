export interface ChunkRecord {
  chunk_id: string;
  document_id: string;
  parent_document_id: string | null;
  content: string;
  embedding_text: string;
  embedding_text_hash: string;
  metadata: {
    title?: string;
    source?: string;
    page_id?: string | null;
    root_page_id?: string | null;
    depth?: number;
    breadcrumbs?: string[];
    heading_path?: string[];
    section_title?: string;
    token_count?: number;
    contains_table?: boolean;
    assets?: unknown[];
    duplicate_of_chunk_id?: string | null;
    [key: string]: unknown;
  };
}

export interface EvalQuery {
  id: string;
  query: string;
  expected_document_ids?: string[];
  expected_chunk_ids?: string[];
}

export interface TeiInfo {
  model_id?: string;
  model_sha?: string;
  model_dtype?: string;
  max_input_length?: number;
  max_batch_requests?: number;
  max_client_batch_size?: number;
  auto_truncate?: boolean;
  version?: string;
  backend?: string;
  [key: string]: unknown;
}

export interface CollectionSnapshot {
  name: string;
  status: string;
  pointsCount: number;
  dimension: number;
  distance: string;
  metadata: Record<string, unknown>;
  payloadSchema: Record<string, unknown>;
}

export interface StoredPoint {
  id: string;
  payload: Record<string, unknown>;
  vector: number[];
}

export interface SearchHit {
  id: string;
  score: number;
  payload: Record<string, unknown>;
}
