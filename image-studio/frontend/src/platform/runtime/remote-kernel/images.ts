import { buildImagesRequestBody } from "./requestPayloads.ts";
import {
  nowSeconds,
  normalizeBaseURL,
  registerRawText,
  resolveSourceDataURLs,
  shouldUseAndroidNativeHTTP,
  sleepWithSignal,
} from "./common.ts";
import { nativeHttpRequestText } from "./nativeHttp.ts";
import {
  RemoteKernelError,
  STATUS_INTERVAL_MS,
  type ExtractedImageResult,
  type RemoteJobCallbacks,
  type RemoteJobRequest,
  type RemoteJobResult,
} from "./types.ts";

const IMAGES_TASK_POLL_INTERVAL_MS = 3_000;
const IMAGES_TASK_POLL_TIMEOUT_MS = 30 * 60_000;
const IMAGES_TASK_COMPLETED_RESULT_GRACE_MS = 120_000;

function parseSSEEvent(line: string): any | null {
  const stripped = line.trim();
  if (!stripped.startsWith("data: ")) return null;
  const payload = stripped.slice(6).trim();
  if (!payload || payload === "[DONE]") return null;
  try {
    return JSON.parse(payload);
  } catch {
    return null;
  }
}

function parseNativeProgressPayload(payload: unknown): { line: string; event: any | null } {
  if (typeof payload === "string") {
    return { line: payload, event: parseSSEEvent(payload) };
  }
  if (!payload || typeof payload !== "object") {
    return { line: "", event: null };
  }
  const line = typeof (payload as { line?: unknown }).line === "string"
    ? (payload as { line: string }).line
    : "";
  const structured = (payload as { event?: unknown }).event;
  const event = structured && typeof structured === "object"
    ? structured
    : parseSSEEvent(line);
  return { line, event };
}

function parseImagesStreamEvent(
  event: any,
  callbacks: RemoteJobCallbacks,
): ExtractedImageResult | null {
  const type = event?.type;
  if (type === "image_generation.partial_image" || type === "image_edit.partial_image") {
    if (event.b64_json) {
      callbacks.onPartialImage?.({
        imageB64: event.b64_json,
        partialImageIndex: typeof event.partial_image_index === "number" ? event.partial_image_index : undefined,
        sourceEvent: "images_partial",
      });
    }
    return null;
  }
  if (type === "image_generation.completed" || type === "image_edit.completed") {
    if (event.b64_json) {
      return {
        imageB64: event.b64_json,
        revisedPrompt: "",
        sourceEvent: "images_api",
      };
    }
  }
  if (event?.object === "image.generation.result" || event?.object === "image.edit.result") {
    return parseImagesResponse(JSON.stringify(event), 200);
  }
  return null;
}

type ImagesTaskRef = {
  id: string;
  status: string;
};

type ParsedImagesResponse = {
  result?: ExtractedImageResult;
  task?: ImagesTaskRef;
  imageURL?: string;
  revisedPrompt?: string;
};

function taskIDFromParsed(parsed: any): string {
  return String(parsed?.task_id || parsed?.id || "").trim();
}

function normalizeTaskStatus(status: unknown): string {
  return String(status || "").trim().toLowerCase();
}

function isPendingTaskStatus(status: string): boolean {
  return ["", "queued", "pending", "processing", "running", "in_progress", "submitted"].includes(normalizeTaskStatus(status));
}

function isFailedTaskStatus(status: string): boolean {
  return ["failed", "cancelled", "canceled", "expired", "error"].includes(normalizeTaskStatus(status));
}

function isCompletedTaskStatus(status: string): boolean {
  return ["completed", "succeeded", "success", "done"].includes(normalizeTaskStatus(status));
}

function isTransientPollHTTPStatus(status: number): boolean {
  return status === 408 || status === 409 || status === 425 || status === 429 || status >= 500;
}

function firstImageDatum(parsed: any): any | null {
  if (Array.isArray(parsed?.data) && parsed.data.length > 0) return parsed.data[0];
  if (parsed?.detail && typeof parsed.detail === "object") return firstImageDatum(parsed.detail);
  if (parsed?.result && typeof parsed.result === "object") return firstImageDatum(parsed.result);
  return null;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode(...chunk);
  }
  return btoa(binary);
}

function imagesTaskPollURL(baseURL: string, taskID: string): string {
  const url = new URL(`v1/images/${encodeURIComponent(taskID)}`, `${normalizeBaseURL(baseURL)}/`);
  url.searchParams.set("detail", "true");
  return url.toString();
}

function parseImagesResponseOrTask(raw: string, status: number, allowAsyncTask: boolean): ParsedImagesResponse {
  let parsed: any;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    if (status >= 400) {
      throw new RemoteKernelError(`上游返回 HTTP ${status}: ${raw.slice(0, 400)}`);
    }
    throw new RemoteKernelError(`解析 Images API 响应失败:${(error as any)?.message || error}`);
  }
  if (status >= 400) {
    if (parsed?.error?.message) {
      throw new RemoteKernelError(`上游返回 ${status}:${parsed.error.message}`);
    }
    throw new RemoteKernelError(`上游返回 HTTP ${status}`);
  }
  if (parsed?.error?.message) {
    throw new RemoteKernelError(`上游返回错误:${parsed.error.message}`);
  }
  const first = firstImageDatum(parsed);
  if (first?.b64_json) {
    return {
      result: {
        imageB64: first.b64_json,
        revisedPrompt: first.revised_prompt || "",
        sourceEvent: "images_api",
      },
    };
  }
  if (first && !first.b64_json) {
    const imageURL = String(first.download_url || first.url || "").trim();
    if (imageURL) {
      return { imageURL, revisedPrompt: first.revised_prompt || "" };
    }
    throw new RemoteKernelError("上游没有返回可用图片");
  }
  const taskID = taskIDFromParsed(parsed);
  const taskStatus = normalizeTaskStatus(parsed?.status);
  if (allowAsyncTask && taskID && !isFailedTaskStatus(taskStatus)) {
    return { task: { id: taskID, status: taskStatus } };
  }
  if (isFailedTaskStatus(taskStatus)) {
    throw new RemoteKernelError(`Images API 异步任务失败:${parsed?.status || "failed"}`);
  }
  throw new RemoteKernelError("上游没有返回可用图片");
}

async function downloadImagesAPIURL(
  imageURL: string,
  request: RemoteJobRequest,
  callbacks: RemoteJobCallbacks,
  proxyMode: "none" | "custom" | "system",
  revisedPrompt = "",
): Promise<ExtractedImageResult> {
  let parsedURL: URL;
  try {
    parsedURL = new URL(imageURL);
  } catch {
    throw new RemoteKernelError(`上游返回的图片下载地址无效:${imageURL}`);
  }
  if (parsedURL.protocol !== "http:" && parsedURL.protocol !== "https:") {
    throw new RemoteKernelError(`上游返回的图片下载地址协议不支持:${parsedURL.protocol.replace(/:$/, "")}`);
  }
  if (shouldUseAndroidNativeHTTP()) {
    const response = await nativeHttpRequestText(
      imageURL,
      "GET",
      { Accept: "image/*,*/*" },
      "",
      callbacks.signal,
      undefined,
      { proxyMode, proxyURL: request.payload.proxyURL || "", responseBodyEncoding: "base64" },
    );
    if (response.status < 200 || response.status >= 300) {
      throw new RemoteKernelError(`下载异步任务图片返回 HTTP ${response.status}`);
    }
    return {
      imageB64: response.body,
      revisedPrompt,
      sourceEvent: "images_api",
    };
  }
  if (proxyMode !== "system") {
    throw new RemoteKernelError("当前远程内核不能控制代理,请切回本地内核或使用 Android 原生运行");
  }
  const response = await fetch(imageURL, {
    method: "GET",
    headers: { Accept: "image/*,*/*" },
    signal: callbacks.signal,
  });
  if (!response.ok) {
    throw new RemoteKernelError(`下载异步任务图片返回 HTTP ${response.status}`);
  }
  const buffer = await response.arrayBuffer();
  if (buffer.byteLength === 0) {
    throw new RemoteKernelError("下载异步任务图片为空");
  }
  return {
    imageB64: bytesToBase64(new Uint8Array(buffer)),
    revisedPrompt,
    sourceEvent: "images_api",
  };
}

function parseImagesResponse(raw: string, status: number): ExtractedImageResult {
  const parsed = parseImagesResponseOrTask(raw, status, false);
  if (!parsed.result) throw new RemoteKernelError("上游没有返回可用图片");
  return parsed.result;
}

function parseImagesStreamRaw(
  raw: string,
  callbacks: RemoteJobCallbacks,
  emitPartials = false,
): ExtractedImageResult | null {
  const partialCallbacks = emitPartials ? callbacks : { signal: callbacks.signal };
  for (const line of raw.split(/\r?\n/)) {
    const event = parseSSEEvent(line);
    if (!event) continue;
    const result = parseImagesStreamEvent(event, partialCallbacks);
    if (result) return result;
  }
  return null;
}

async function requestImagesTaskStatus(
  url: string,
  request: RemoteJobRequest,
  callbacks: RemoteJobCallbacks,
  proxyMode: "none" | "custom" | "system",
): Promise<{ raw: string; status: number; contentType: string }> {
  if (shouldUseAndroidNativeHTTP()) {
    const response = await nativeHttpRequestText(
      url,
      "GET",
      {
        Authorization: `Bearer ${request.payload.apiKey}`,
        Accept: "application/json",
      },
      "",
      callbacks.signal,
      undefined,
      { proxyMode, proxyURL: request.payload.proxyURL || "" },
    );
    return {
      raw: response.body,
      status: response.status,
      contentType: response.contentType || "",
    };
  }
  if (proxyMode !== "system") {
    throw new RemoteKernelError("当前远程内核不能控制代理,请切回本地内核或使用 Android 原生运行");
  }
  const response = await fetch(url, {
    method: "GET",
    headers: {
      Authorization: `Bearer ${request.payload.apiKey}`,
      Accept: "application/json",
    },
    signal: callbacks.signal,
  });
  return {
    raw: await response.text(),
    status: response.status,
    contentType: response.headers.get("content-type") || "",
  };
}

async function pollImagesTask(
  request: RemoteJobRequest,
  initialTask: ImagesTaskRef,
  startedAt: number,
  callbacks: RemoteJobCallbacks,
  proxyMode: "none" | "custom" | "system",
): Promise<{ result: ExtractedImageResult; raw: string }> {
  const deadline = Date.now() + IMAGES_TASK_POLL_TIMEOUT_MS;
  const taskURL = imagesTaskPollURL(request.payload.baseURL, initialTask.id);
  let rawLog = `\n\n--- images-task-${initialTask.id}-submitted ---\n${JSON.stringify(initialTask)}\n`;
  let lastStatus = initialTask.status;
  let completedWithoutResultSince = 0;
  for (let attempt = 1; ; attempt++) {
    if (attempt > 1) await sleepWithSignal(callbacks.signal, IMAGES_TASK_POLL_INTERVAL_MS);
    if (Date.now() > deadline) {
      throw new RemoteKernelError(lastStatus
        ? `Images API 异步任务轮询超时,最后状态:${lastStatus}`
        : "Images API 异步任务轮询超时");
    }
    callbacks.onProgress?.(lastStatus ? `Images API 异步任务处理中:${lastStatus}` : "Images API 异步任务处理中", nowSeconds(startedAt), 0);
    let response: Awaited<ReturnType<typeof requestImagesTaskStatus>>;
    try {
      response = await requestImagesTaskStatus(taskURL, request, callbacks, proxyMode);
    } catch (error) {
      callbacks.onProgress?.(`Images API 异步任务轮询请求失败，继续等待:${(error as any)?.message || error}`, nowSeconds(startedAt), 0);
      continue;
    }
    rawLog += `\n\n--- images-task-${initialTask.id}-poll-${attempt} ---\n${response.raw}`;
    if (!response.raw.trim()) {
      if (response.status < 200 || response.status >= 300) {
        if (isTransientPollHTTPStatus(response.status)) {
          callbacks.onProgress?.(`上游轮询返回 HTTP ${response.status} 空响应，继续等待`, nowSeconds(startedAt), 0);
          continue;
        }
        throw new RemoteKernelError(`上游轮询返回 HTTP ${response.status} 空响应`);
      }
      callbacks.onProgress?.("Images API 异步任务轮询返回空响应，继续等待", nowSeconds(startedAt), 0);
      continue;
    }
    let parsed: ParsedImagesResponse;
    try {
      parsed = parseImagesResponseOrTask(response.raw, response.status, true);
    } catch (error) {
      if (isTransientPollHTTPStatus(response.status)) {
        callbacks.onProgress?.(`上游轮询返回 HTTP ${response.status} 临时错误，继续等待`, nowSeconds(startedAt), 0);
        continue;
      }
      throw error;
    }
    if (parsed.result) return { result: parsed.result, raw: rawLog };
    if (parsed.imageURL) {
      callbacks.onProgress?.("下载 Images API 异步任务图片", nowSeconds(startedAt), 0);
      const result = await downloadImagesAPIURL(parsed.imageURL, request, callbacks, proxyMode, parsed.revisedPrompt || "");
      return { result, raw: rawLog };
    }
    if (!parsed.task) throw new RemoteKernelError("上游没有返回可用图片");
    lastStatus = parsed.task.status;
    if (isFailedTaskStatus(lastStatus)) {
      throw new RemoteKernelError(`Images API 异步任务失败:${lastStatus}`);
    }
    if (isCompletedTaskStatus(lastStatus)) {
      if (!completedWithoutResultSince) completedWithoutResultSince = Date.now();
      if (Date.now() - completedWithoutResultSince <= IMAGES_TASK_COMPLETED_RESULT_GRACE_MS) {
        callbacks.onProgress?.("Images API 异步任务已完成，等待图片结果写入", nowSeconds(startedAt), 0);
        continue;
      }
      throw new RemoteKernelError("Images API 异步任务已完成但没有返回可用图片");
    }
    completedWithoutResultSince = 0;
    if (!isPendingTaskStatus(lastStatus)) {
      throw new RemoteKernelError(`Images API 异步任务状态未知:${lastStatus}`);
    }
  }
}

export async function requestImagesOnce(
  request: RemoteJobRequest,
  attempt: number,
  maxAttempts: number,
  callbacks: RemoteJobCallbacks,
): Promise<RemoteJobResult> {
  const sourceDataURLs = await resolveSourceDataURLs(request.sourceImages, request.payload);
  const built = await buildImagesRequestBody(request, sourceDataURLs);
  const startedAt = Date.now();
  callbacks.onLog?.(`[Images API] 第 ${attempt}/${maxAttempts} 次请求...`);
  callbacks.onProgress?.("等待 Images API 返回(无 SSE 保活)", 0, 0);
  const ticker = globalThis.setInterval(() => {
    callbacks.onProgress?.("等待 Images API 返回(无 SSE 保活)", nowSeconds(startedAt), 0);
  }, STATUS_INTERVAL_MS);
  try {
    const proxyMode = request.payload.proxyMode === "none" || request.payload.proxyMode === "custom" ? request.payload.proxyMode : "system";
    if (shouldUseAndroidNativeHTTP()) {
      let rawFromLines = "";
      let nativeStreamResult: ExtractedImageResult | null = null;
      let nativeBytesReceived = 0;
      let receivedNativeStreamPayload = false;
      const consumeNativePayload = (payload: unknown) => {
        receivedNativeStreamPayload = true;
        const parsedPayload = parseNativeProgressPayload(payload);
        if (parsedPayload.line) {
          rawFromLines += `${parsedPayload.line}\n`;
          nativeBytesReceived += parsedPayload.line.length + 1;
        }
        const parsed = parsedPayload.event ? parseImagesStreamEvent(parsedPayload.event, callbacks) : null;
        if (parsed) nativeStreamResult = parsed;
        callbacks.onProgress?.("已收到 Images API 流式事件", nowSeconds(startedAt), nativeBytesReceived);
      };
      const response = await nativeHttpRequestText(
        built.url,
        "POST",
        {
          Authorization: `Bearer ${request.payload.apiKey}`,
          Accept: "text/event-stream, application/json",
          ...(built.headers ?? {}),
        },
        built.body,
        callbacks.signal,
        consumeNativePayload,
        { proxyMode, proxyURL: request.payload.proxyURL || "" },
      );
      const rawBody = response.body || rawFromLines;
      const rawPath = response.rawPath || registerRawText("images", attempt, rawBody);
      const isStream = String(response.contentType || "").toLowerCase().includes("text/event-stream");
      if (response.resultImageB64) {
        return {
          imageB64: response.resultImageB64,
          revisedPrompt: response.revisedPrompt || "",
          sourceEvent: response.sourceEvent || "images_api",
          rawPath,
          prompt: request.payload.prompt,
          mode: request.payload.mode,
        };
      }
      const parsedResult = isStream
        ? nativeStreamResult ?? (receivedNativeStreamPayload ? null : parseImagesStreamRaw(rawBody, callbacks))
        : parseImagesResponseOrTask(rawBody, response.status, request.payload.imagesAsyncPolling === true).result;
      let result = parsedResult;
      let finalRawPath = rawPath;
      if (!isStream && !result) {
        const parsed = parseImagesResponseOrTask(rawBody, response.status, request.payload.imagesAsyncPolling === true);
        if (parsed.imageURL) {
          callbacks.onProgress?.("下载 Images API 异步任务图片", nowSeconds(startedAt), 0);
          result = await downloadImagesAPIURL(parsed.imageURL, request, callbacks, proxyMode, parsed.revisedPrompt || "");
        } else if (parsed.task) {
          const polled = await pollImagesTask(request, parsed.task, startedAt, callbacks, proxyMode);
          result = polled.result;
          finalRawPath = registerRawText("images", attempt, `${rawBody}${polled.raw}`);
        }
      }
      if (!result) throw new RemoteKernelError("上游没有返回可用图片", finalRawPath);
      return { ...result, rawPath: finalRawPath, prompt: request.payload.prompt, mode: request.payload.mode };
    }
    if (proxyMode !== "system") {
      throw new RemoteKernelError("当前远程内核不能控制代理,请切回本地内核或使用 Android 原生运行");
    }
    const response = await fetch(built.url, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${request.payload.apiKey}`,
        Accept: "text/event-stream, application/json",
        ...(built.headers ?? {}),
      },
      body: built.body,
      signal: callbacks.signal,
    });
    const contentType = response.headers.get("content-type")?.toLowerCase() || "";
    if (response.body && contentType.includes("text/event-stream")) {
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let raw = "";
      let pending = "";
      let result: ExtractedImageResult | null = null;
      let bytesReceived = 0;
      try {
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          bytesReceived += value.byteLength;
          const chunk = decoder.decode(value, { stream: true });
          raw += chunk;
          pending += chunk;
          let newline = pending.indexOf("\n");
          while (newline >= 0) {
            const line = pending.slice(0, newline).replace(/\r$/, "");
            pending = pending.slice(newline + 1);
            const event = parseSSEEvent(line);
            const parsed = event ? parseImagesStreamEvent(event, callbacks) : null;
            if (parsed) result = parsed;
            callbacks.onProgress?.("已收到 Images API 流式事件", nowSeconds(startedAt), bytesReceived);
            newline = pending.indexOf("\n");
          }
        }
        raw += decoder.decode();
        if (pending.trim()) {
          const event = parseSSEEvent(pending);
          const parsed = event ? parseImagesStreamEvent(event, callbacks) : null;
          if (parsed) result = parsed;
        }
      } catch (error) {
        const fallback = parseImagesStreamRaw(raw, callbacks);
        if (fallback?.imageB64) {
          const rawPath = registerRawText("images", attempt, raw);
          return { ...fallback, rawPath, prompt: request.payload.prompt, mode: request.payload.mode };
        }
        throw error;
      }
      const rawPath = registerRawText("images", attempt, raw);
      if (!response.ok) {
        throw new RemoteKernelError(`上游返回 HTTP ${response.status}`, rawPath);
      }
      result ??= parseImagesStreamRaw(raw, callbacks);
      if (!result) throw new RemoteKernelError("上游没有返回可用图片", rawPath);
      return { ...result, rawPath, prompt: request.payload.prompt, mode: request.payload.mode };
    }
    const raw = await response.text();
    const rawPath = registerRawText("images", attempt, raw);
    const parsed = parseImagesResponseOrTask(raw, response.status, request.payload.imagesAsyncPolling === true);
    if (parsed.result) {
      return { ...parsed.result, rawPath, prompt: request.payload.prompt, mode: request.payload.mode };
    }
    if (parsed.imageURL) {
      callbacks.onProgress?.("下载 Images API 异步任务图片", nowSeconds(startedAt), 0);
      const result = await downloadImagesAPIURL(parsed.imageURL, request, callbacks, proxyMode, parsed.revisedPrompt || "");
      return { ...result, rawPath, prompt: request.payload.prompt, mode: request.payload.mode };
    }
    if (parsed.task) {
      const polled = await pollImagesTask(request, parsed.task, startedAt, callbacks, proxyMode);
      const finalRawPath = registerRawText("images", attempt, `${raw}${polled.raw}`);
      return { ...polled.result, rawPath: finalRawPath, prompt: request.payload.prompt, mode: request.payload.mode };
    }
    throw new RemoteKernelError("上游没有返回可用图片", rawPath);
  } catch (error) {
    if (error instanceof RemoteKernelError) throw error;
    throw new RemoteKernelError(String((error as any)?.message || error));
  } finally {
    globalThis.clearInterval(ticker);
  }
}
