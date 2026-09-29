export type Role = "user" | "operator" | "admin";

export type FileType = "pdf" | "docx" | "eml" | "xlsx" | "pptx" | "txt" | "image" | "db" | "other";

export interface DocFragment {
  id: string;
  text: string;
  location: string;
  page?: number;
  heading?: string;
}

interface EmailMeta {
  from: string;
  to: string;
  subject: string;
  date: string;
}

export interface KbDoc {
  id: string;
  /** Фрагменты — настоящие чанки индекса: режим чтения не добавляет вокруг синтетический текст. */
  real?: boolean;
  /** Неизменяемый индекс источника, на который ссылается сохранённый ответ. */
  sourceSnapshot?: string;
  hasProvenance?: boolean;
  title: string;
  snippet: string;
  section: string;
  sourceId: string;
  sourceName: string;
  updatedAt: string;
  tags: string[];
  fileType: FileType;
  fileName: string;
  author?: string;
  publishedAt?: string;
  emailMeta?: EmailMeta;
  fragments: DocFragment[];
}

export interface SmartAnswerSource {
  doc: KbDoc;
  fragmentId: string;
  relevance?: number;
  page?: string;
  section?: string;
}

export interface AnswerChip {
  label: number;
  docId: string;
  fragmentId: string;
}

export interface SmartAnswer {
  /** Markdown ответа; маркеры [Sn] заменены ссылками вида [n](#cite-i). */
  markdown: string;
  plain: string;
  chips: AnswerChip[];
  sources: SmartAnswerSource[];
}
