export interface IndexerError {
  line: number | null;
  chunk_id: string | null;
  stage: "input" | "validation" | "tokenization" | "embedding" | "qdrant" | "prune";
  message: string;
}

export interface IndexerReport {
  input_file: string;
  collection: string;
  embedding_model: string;
  embedding_model_revision: string;
  embedding_pipeline_version: string;
  batch_size: number;
  dry_run: boolean;
  input_chunks: number;
  valid_chunks: number;
  new_chunks: number;
  changed_embeddings: number;
  embedded_chunks: number;
  reused_vectors_in_run: number;
  payload_only_updates: number;
  skipped_unchanged: number;
  stale_chunks_found: number;
  deleted_chunks: number;
  failed_chunks: number;
  model_loaded: boolean;
  model_load_ms: number;
  embedding_ms: number;
  qdrant_read_ms: number;
  qdrant_write_ms: number;
  total_ms: number;
  prune_skipped_due_to_errors: boolean;
  errors: IndexerError[];
}
