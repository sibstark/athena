export type DocumentType = "markdown" | "external_link" | "short_note" | "empty";

export interface NormalizedAsset {
  type: "image";
  path: string;
  alt: string;
}

export interface NormalizedMetadata {
  title: string | null;
  source: string;
  page_id: string | null;
  parent_page_id: string | null;
  root_page_id: string | null;
  depth: number;
  ancestor_ids: string[];
  breadcrumbs: string[];
  context_text: string;
  document_type: DocumentType;
  indexable: boolean;
  external_urls: string[];
  assets: NormalizedAsset[];
  version: string | null;
  date: string | null;
  category: string | null;
  status: string;
  language: string;
  [key: string]: unknown;
}

export interface NormalizedDocument {
  document_id: string;
  parent_document_id: string | null;
  content: string;
  metadata: NormalizedMetadata;
  content_hash: string;
}

export interface DocumentTreeNode {
  node_id: string;
  parent_node_id: string | null;
  page_id: string | null;
  title: string;
  depth: number;
  has_document: boolean;
  source: string | null;
}

export interface DocumentTree {
  nodes: DocumentTreeNode[];
}
