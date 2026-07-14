export interface BitrixTimeInfo {
  start?: number;
  finish?: number;
  duration?: number;
  processing?: number;
}

export interface BitrixEnvelope<T> {
  result?: T;
  error?: string;
  error_description?: string;
  time?: BitrixTimeInfo;
}

export interface CollectionCursor {
  position: number;
  id: number;
}

export interface KnowledgeCollection {
  id: number;
  name: string;
  position: number;
  policyLevel: "none" | "view" | "manage" | "moderate" | string;
  createdBy: number;
  updatedBy: number;
  createdAt: string;
  updatedAt: string;
}

export interface CollectionListResult {
  items: KnowledgeCollection[];
  nextCursor: CollectionCursor | null;
}

export interface DocumentTreeNode {
  id: number;
  collectionId: number;
  parentId: number | null;
  title: string;
  position: number;
  children: DocumentTreeNode[];
}

export interface DocumentTreeResult {
  items: DocumentTreeNode[];
  truncated: boolean;
}

export interface KnowledgeDocument {
  id: number;
  collectionId: number | null;
  parentId: number | null;
  title: string;
  markdown: string;
  position: number;
  createdBy: number;
  updatedBy: number;
  createdAt: string;
  updatedAt: string;
}

export interface DocumentGetResult {
  item: KnowledgeDocument;
}
