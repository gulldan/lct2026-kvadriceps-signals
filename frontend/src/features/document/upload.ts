import { i18n } from "@/shared/i18n";
import {
  abortUpload,
  beginUpload,
  completeUpload,
  getUploadPartUrls,
  uploadDocument,
  type ApiDocument,
  type UploadPartRef,
} from "./api";

/** Не выше лимита прямой загрузки сервера (MAX_UPLOAD_MB в main-service, по умолчанию 50 МиБ):
 *  больший файл прямым POST сервер отвергнет. */
const DIRECT_LIMIT_BYTES = 50 * 1024 * 1024;

const PART_CONCURRENCY = 3;
const PART_RETRIES = 3;
const URL_BATCH = 200;

export interface UploadProgress {
  fraction: number;
  uploadedBytes: number;
  totalBytes: number;
}

export class UploadCancelled extends Error {
  constructor() {
    super(i18n.t("document:upload.cancelled"));
  }
}

/** fetch не умеет прогресс отправки, поэтому XHR. */
function putPart(
  url: string,
  blob: Blob,
  onBytes: (sent: number) => void,
  signal: AbortSignal,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    let lastSent = 0;
    const onAbort = () => {
      xhr.abort();
      reject(new UploadCancelled());
    };
    signal.addEventListener("abort", onAbort, { once: true });
    xhr.upload.addEventListener("progress", (e) => {
      onBytes(e.loaded - lastSent);
      lastSent = e.loaded;
    });
    xhr.addEventListener("load", () => {
      signal.removeEventListener("abort", onAbort);
      if (xhr.status >= 200 && xhr.status < 300) {
        const etag = xhr.getResponseHeader("ETag");
        if (!etag) {
          reject(new Error(i18n.t("document:upload.noEtag")));
          return;
        }
        resolve(etag);
      } else {
        reject(new Error(i18n.t("document:upload.partHttp", { status: xhr.status })));
      }
    });
    xhr.addEventListener("error", () => {
      signal.removeEventListener("abort", onAbort);
      reject(new Error(i18n.t("document:upload.partNetwork")));
    });
    xhr.open("PUT", url);
    xhr.send(blob);
  });
}

async function putPartWithRetry(
  url: string,
  blob: Blob,
  onBytes: (sent: number) => void,
  signal: AbortSignal,
): Promise<string> {
  let lastError: unknown;
  for (let attempt = 1; attempt <= PART_RETRIES; attempt++) {
    if (signal.aborted) throw new UploadCancelled();
    try {
      return await putPart(url, blob, onBytes, signal);
    } catch (err) {
      if (err instanceof UploadCancelled) throw err;
      lastError = err;
      // Прогресс неудачной попытки не откатывается: скачок заметен меньше, чем полоса назад.
      await new Promise((r) => setTimeout(r, attempt * 1000));
    }
  }
  throw lastError instanceof Error ? lastError : new Error(i18n.t("document:upload.partFailed"));
}

async function uploadLarge(
  file: File,
  onProgress: (p: UploadProgress) => void,
  signal: AbortSignal,
  reuse = false,
): Promise<ApiDocument> {
  const session = await beginUpload(file.name, file.size, file.type, reuse);
  let uploaded = 0;
  const report = (delta: number) => {
    uploaded += delta;
    onProgress({
      fraction: Math.min(1, uploaded / file.size),
      uploadedBytes: Math.min(uploaded, file.size),
      totalBytes: file.size,
    });
  };

  const etags = new Map<number, string>();
  try {
    for (let batchFrom = 1; batchFrom <= session.part_count; batchFrom += URL_BATCH) {
      const { urls } = await getUploadPartUrls(session.upload_id, batchFrom, URL_BATCH);
      let next = 0;
      const worker = async () => {
        for (;;) {
          const part = urls[next++];
          if (part === undefined) return;
          const { part_number, url } = part;
          const start = (part_number - 1) * session.part_size;
          const blob = file.slice(start, Math.min(start + session.part_size, file.size));
          const etag = await putPartWithRetry(url, blob, report, signal);
          etags.set(part_number, etag);
        }
      };
      await Promise.all(Array.from({ length: Math.min(PART_CONCURRENCY, urls.length) }, worker));
    }

    const parts: UploadPartRef[] = [...etags.entries()]
      .toSorted((a, b) => a[0] - b[0])
      .map(([part_number, etag]) => ({ part_number, etag }));
    return await completeUpload(session.upload_id, parts);
  } catch (err) {
    // Ошибку отмены глотаем: сессия всё равно истечёт по TTL.
    void abortUpload(session.upload_id).catch(() => undefined);
    throw err;
  }
}

export async function uploadSmart(
  file: File,
  onProgress: (p: UploadProgress) => void,
  signal: AbortSignal,
  reuse = false,
): Promise<ApiDocument> {
  if (file.size <= DIRECT_LIMIT_BYTES) {
    const doc = await uploadDocument(file, reuse);
    onProgress({ fraction: 1, uploadedBytes: file.size, totalBytes: file.size });
    return doc;
  }
  return uploadLarge(file, onProgress, signal, reuse);
}
