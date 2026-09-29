import { ApiError, authFetch, postJSON, request } from "@/shared/api/client";
import type { SourceMetadata } from "./sourceApi";

export const UPLOAD_ACCEPT =
  ".pdf,.doc,.docx,.eml,.msg,.xls,.xlsx,.ppt,.pptx,.txt,.md,.png,.jpg,.jpeg,.webp,.tiff,.db,.sqlite,.sqlite3,.zip,.rar,.7z,.tar,.gz,.tgz";

export type DocStatus =
  | "uploaded"
  | "queued"
  | "parsing"
  | "ocr"
  | "parsed"
  | "chunking"
  | "indexed"
  | "failed";

export interface ApiDocument {
  id: string;
  owner_id: string;
  source_metadata?: SourceMetadata;
  filename: string;
  title?: string;
  author?: string;
  published_at?: string;
  source_ref?: string;
  /** "hypotheses" — готовые гипотезы; они исключены из генерации и проверки. */
  kind?: string;
  mime_type: string;
  size: number;
  object_key: string;
  status: DocStatus;
  status_msg?: string;
  chunk_count: number;
  created_at: string;
  updated_at: string;
  duplicate?: boolean;
}

export interface ApiChunk {
  id: string;
  index: number;
  text: string;
}

export interface ApiDocumentChunks {
  document: ApiDocument;
  chunks: ApiChunk[];
}

export interface IngestionEvent {
  document_id: string;
  owner_id: string;
  status: DocStatus;
  message?: string;
  timestamp: string;
  /** Прогресс внутри стадии (страницы OCR, батчи индексации); 0 — неизвестен. */
  stage_current?: number;
  stage_total?: number;
}

export function getDocument(id: string): Promise<ApiDocument> {
  return request<ApiDocument>(`/documents/${id}`);
}

export function getDocumentChunks(id: string, snapshot?: string): Promise<ApiDocumentChunks> {
  const query = snapshot ? `?snapshot=${encodeURIComponent(snapshot)}` : "";
  return request<ApiDocumentChunks>(`/documents/${encodeURIComponent(id)}/chunks${query}`);
}

export function uploadDocument(file: File, reuse = false): Promise<ApiDocument> {
  const form = new FormData();
  form.append("file", file);
  const path = reuse ? "/documents?reuse=1" : "/documents";
  return request<ApiDocument>(path, { method: "POST", body: form });
}

/** Вызывающий обязан освободить URL через URL.revokeObjectURL при закрытии просмотра. */
export async function fetchDocumentContent(
  id: string,
  snapshot?: string,
  signal?: AbortSignal,
): Promise<{ url: string; mime: string }> {
  const path = snapshot
    ? `/documents/${encodeURIComponent(id)}/source/content?snapshot=${encodeURIComponent(snapshot)}`
    : `/documents/${encodeURIComponent(id)}/content`;
  const resp = await authFetch(path, { signal });
  if (!resp.ok) throw new ApiError(resp.status, `${resp.status} ${resp.statusText}`);
  const mime = resp.headers.get("Content-Type") ?? "application/octet-stream";
  return { url: URL.createObjectURL(await resp.blob()), mime };
}

export interface UploadSession {
  upload_id: string;
  part_size: number;
  part_count: number;
}

export interface UploadPartRef {
  part_number: number;
  etag: string;
}

export function beginUpload(
  filename: string,
  size: number,
  mimeType: string,
  reuse = false,
): Promise<UploadSession> {
  return postJSON<UploadSession>("/uploads", { filename, size, mime_type: mimeType, reuse });
}

export function getUploadPartUrls(
  uploadId: string,
  from: number,
  count: number,
): Promise<{ urls: { part_number: number; url: string }[] }> {
  return request(`/uploads/${uploadId}/parts?from=${from}&count=${count}`);
}

export function completeUpload(uploadId: string, parts: UploadPartRef[]): Promise<ApiDocument> {
  return postJSON<ApiDocument>(`/uploads/${uploadId}/complete`, { parts });
}

export function abortUpload(uploadId: string): Promise<void> {
  return request<void>(`/uploads/${uploadId}`, { method: "DELETE" });
}
