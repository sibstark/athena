export interface ChunkMetadata {
  title: string | null;
  source: string;
  page_id: string | null;
  parent_page_id: string | null;
  root_page_id: string | null;
  depth: number;
  ancestor_ids: string[];
  breadcrumbs: string[];
  heading_path: string[];
  section_title: string;
  language: string;
  status: string;
  category?: string | null;
  version?: string | null;
  date?: string | null;
  duplicate_of_chunk_id?: string;
  [key: string]: unknown;
}

export interface ChunkRecord {
  chunk_id: string;
  document_id: string;
  parent_document_id: string | null;
  content: string;
  embedding_text: string;
  content_hash: string;
  embedding_text_hash: string;
  metadata: ChunkMetadata;
  previous_chunk_id: string | null;
  next_chunk_id: string | null;
}
