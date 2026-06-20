package client

// images_api.go — 适配标准的 OpenAI Images API:
//   POST {base}/v1/images/generations  (JSON,文生图)
//   POST {base}/v1/images/edits        (multipart/form-data,图生图)
//
// 与 Responses API 路径(client.go / sse.go)的最大区别:
//   - 结果事件形态不同;支持官方 Images API 的 stream/partial_images 时可流式预览,
//     否则回退解析一次性 JSON 响应。
//   - 多图编辑能力受上游约束(OpenAI 官方仅接受 1 张 image,部分中转站允许 image[] 数组),
//     为最大兼容,这里默认只取第一张源图;如果上游支持多张,可后续扩展
//   - 默认优先走 OpenAI 官方公开字段;若请求策略切到 compat,可附带 relay 扩展字段

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func classifyImageModel(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(normalized, "dall-e-2"):
		return "dalle2"
	case strings.HasPrefix(normalized, "dall-e-3"):
		return "dalle3"
	case strings.HasPrefix(normalized, "gpt-image"), strings.HasPrefix(normalized, "chatgpt-image"):
		return "gpt-image"
	default:
		return "other"
	}
}

func supportsImagesResponseFormat(model string, mode Mode) bool {
	family := classifyImageModel(model)
	if mode == ModeEdit {
		return family == "dalle2"
	}
	return family == "dalle2" || family == "dalle3"
}

func supportsImageModeration(model string) bool {
	return classifyImageModel(model) == "gpt-image"
}

func supportsImageBackground(model string) bool {
	return classifyImageModel(model) == "gpt-image"
}

func supportsOutputCompression(model, outputFormat string) bool {
	return supportsImageBackground(model) && (outputFormat == "jpeg" || outputFormat == "webp")
}

func supportsInputFidelity(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(normalized, "gpt-image-2") {
		return false
	}
	if strings.HasPrefix(normalized, "gpt-image-1.5") {
		return true
	}
	if strings.HasPrefix(normalized, "gpt-image-1-mini") {
		return true
	}
	if strings.HasPrefix(normalized, "gpt-image-1") {
		return true
	}
	if strings.HasPrefix(normalized, "chatgpt-image-latest") {
		return true
	}
	return false
}

func supportsImageStyle(model string, mode Mode) bool {
	return mode != ModeEdit && classifyImageModel(model) == "dalle3"
}

func normalizeImageStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "vivid":
		return "vivid"
	case "natural":
		return "natural"
	default:
		return DefaultImageStyle
	}
}

type imagesAPIDatum struct {
	B64JSON       string `json:"b64_json"`
	URL           string `json:"url"`
	DownloadURL   string `json:"download_url"`
	RevisedPrompt string `json:"revised_prompt"`
}

type imagesAPIResponse struct {
	ID      string             `json:"id"`
	TaskID  string             `json:"task_id"`
	Object  string             `json:"object"`
	Model   string             `json:"model"`
	Status  string             `json:"status"`
	Created int                `json:"created"`
	Data    []imagesAPIDatum   `json:"data"`
	Detail  *imagesAPIResponse `json:"detail,omitempty"`
	Result  *imagesAPIResponse `json:"result,omitempty"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type imageStreamExtractor struct {
	partialB64 string
	final      ImageResult
	hasFinal   bool
	onPartial  func(PartialImage)
}

func (e *imageStreamExtractor) consume(line string) bool {
	stripped := strings.TrimSpace(line)
	if stripped == "" {
		return false
	}
	if !strings.HasPrefix(stripped, "data: ") {
		return false
	}
	payload := strings.TrimSpace(stripped[6:])
	if payload == "" || payload == "[DONE]" {
		return true
	}
	var ev Event
	if err := decodeEvent(payload, &ev); err != nil {
		return false
	}
	evType, _ := ev["type"].(string)
	switch evType {
	case "image_generation.partial_image", "image_edit.partial_image":
		if b64, ok := ev["b64_json"].(string); ok && b64 != "" {
			e.partialB64 = b64
			partial := PartialImage{ImageB64: b64, PartialImageIndex: -1}
			if idx, ok := numberFromAny(ev["partial_image_index"]); ok {
				partial.PartialImageIndex = idx
			}
			if e.onPartial != nil {
				e.onPartial(partial)
			}
		}
		return true
	case "image_generation.completed", "image_edit.completed":
		if b64, ok := ev["b64_json"].(string); ok && b64 != "" {
			e.final = ImageResult{ImageB64: b64, SourceEvent: "images_api"}
			e.hasFinal = true
			return true
		}
	case "error":
		return true
	}
	if ev["object"] == "image.generation.result" || ev["object"] == "image.edit.result" {
		b, err := json.Marshal(ev)
		if err == nil {
			if result, err := parseImagesAPIResponseBytes(b, 200); err == nil {
				e.final = result
				e.hasFinal = true
				return true
			}
		}
	}
	return true
}

func (e *imageStreamExtractor) result() (ImageResult, bool) {
	if e.hasFinal {
		return e.final, true
	}
	return ImageResult{}, false
}

// RequestImagesAPI executes a single (no-retry) request against the standard
// OpenAI Images API and returns the parsed image. Raw response body is teed
// to rawSink so callers can dump it for debugging.
func RequestImagesAPI(
	ctx context.Context,
	opts Options,
	rawSink io.Writer,
	onProgress func(stage string, elapsedSeconds int, bytesReceived int64),
) (ImageResult, error) {
	return RequestImagesAPIWithPartial(ctx, opts, rawSink, onProgress, nil)
}

func RequestImagesAPIWithPartial(
	ctx context.Context,
	opts Options,
	rawSink io.Writer,
	onProgress func(stage string, elapsedSeconds int, bytesReceived int64),
	onPartial func(PartialImage),
) (ImageResult, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return ImageResult{}, ErrEmptyAPIKey
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return ImageResult{}, ErrEmptyPrompt
	}

	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		return ImageResult{}, errors.New("未配置上游 BASE_URL,请在「设置 → 上游 BASE_URL」中填入兼容 OpenAI Images API 的中转站地址")
	}
	baseURL, err := ValidateBaseURLWithPolicy(baseURL, BaseURLPolicy{AllowInsecureHTTP: opts.AllowInsecureHTTP})
	if err != nil {
		return ImageResult{}, err
	}

	model := opts.ImageModelID
	if model == "" {
		model = ImageModel
	}
	size := opts.Size
	if size == "" {
		size = DefaultSize
	}
	quality := opts.Quality
	if quality == "" {
		quality = DefaultQuality
	}
	outputFormat := opts.OutputFormat
	if outputFormat == "" {
		outputFormat = OutputFormat
	}
	background := normalizeBackground(opts.Background)
	outputCompression := normalizeOutputCompression(opts.OutputCompression)
	inputFidelity := normalizeInputFidelity(opts.InputFidelity)
	imageStyle := normalizeImageStyle(opts.ImageStyle)
	moderation := normalizeModeration(opts.Moderation)
	userIdentifier := normalizeUserIdentifier(opts.UserIdentifier)
	partialImages := normalizePartialImages(opts.PartialImages)
	if opts.DisablePreview {
		partialImages = 0
	}
	includeExtended := shouldSendExtendedImageParameters(opts.RequestPolicy)
	useNewAPICompat := opts.ImagesNewAPICompat
	useAsyncPolling := opts.ImagesAsyncPolling

	var (
		url         string
		body        io.Reader
		contentType string
	)

	if opts.Mode == ModeEdit {
		paths := opts.imageSourcePathsForEdit()
		if len(paths) == 0 {
			return ImageResult{}, errors.New("图生图模式需要至少一张源图(请在面板里添加参考图)")
		}
		multipartBuf, mpType, err := buildEditsMultipart(paths, opts.MaskB64, opts.Prompt, model, size, quality, outputFormat, background, outputCompression, inputFidelity, moderation, userIdentifier, opts.NegativePrompt, opts.Seed, opts.RequestPolicy, partialImages, useNewAPICompat, useAsyncPolling)
		if err != nil {
			return ImageResult{}, err
		}
		url = appendImagesAsyncQuery(baseURL+"/v1/images/edits", useAsyncPolling)
		body = multipartBuf
		contentType = mpType
	} else {
		payload := map[string]any{
			"model":         model,
			"prompt":        opts.Prompt,
			"n":             1,
			"size":          size,
			"quality":       quality,
			"output_format": outputFormat,
		}
		if supportsImageBackground(model) {
			payload["background"] = background
		}
		if supportsOutputCompression(model, outputFormat) {
			payload["output_compression"] = outputCompression
		}
		if supportsImageStyle(model, opts.Mode) && imageStyle != DefaultImageStyle {
			payload["style"] = imageStyle
		}
		if supportsImageModeration(model) {
			payload["moderation"] = moderation
		}
		if userIdentifier != "" {
			payload["user"] = userIdentifier
		}
		if useAsyncPolling {
			payload["async"] = true
		}
		if useNewAPICompat || useAsyncPolling || supportsImagesResponseFormat(model, opts.Mode) {
			payload["response_format"] = "b64_json"
		}
		if !useNewAPICompat && !useAsyncPolling {
			payload["stream"] = true
			payload["partial_images"] = partialImages
		}
		if includeExtended && opts.Seed != 0 {
			payload["seed"] = opts.Seed
		}
		if includeExtended && strings.TrimSpace(opts.NegativePrompt) != "" {
			payload["negative_prompt"] = opts.NegativePrompt
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return ImageResult{}, fmt.Errorf("marshal payload: %w", err)
		}
		url = appendImagesAsyncQuery(baseURL+"/v1/images/generations", useAsyncPolling)
		body = bytes.NewReader(b)
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return ImageResult{}, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+opts.APIKey)
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("User-Agent", UserAgent())

	transport, err := NewHTTPTransport(opts.Proxy)
	if err != nil {
		return ImageResult{}, err
	}
	httpClient := &http.Client{
		Timeout:   8 * time.Minute,
		Transport: transport,
	}

	startedAt := time.Now()
	// Progress ticker — Images API has no streaming so we just tick elapsed time.
	stopProgress := make(chan struct{})
	if onProgress != nil {
		go func() {
			tick := time.NewTicker(time.Duration(StatusIntervalSecond) * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-stopProgress:
					return
				case <-tick.C:
					onProgress("等待 Images API 返回(无 SSE 保活)", int(time.Since(startedAt).Seconds()), 0)
				}
			}
		}()
	}
	defer close(stopProgress)

	resp, err := httpClient.Do(req)
	if err != nil {
		return ImageResult{}, err
	}
	defer resp.Body.Close()

	contentTypeHeader := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentTypeHeader, "text/event-stream") {
		var rawBytes int64
		extractor := imageStreamExtractor{onPartial: onPartial}
		scanner := NewSSEScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			rawBytes += int64(len(line) + 1)
			if _, err := rawSink.Write(line); err != nil {
				return ImageResult{}, fmt.Errorf("write raw: %w", err)
			}
			if _, err := rawSink.Write([]byte("\n")); err != nil {
				return ImageResult{}, fmt.Errorf("write raw: %w", err)
			}
			if extractor.consume(string(line)) && onProgress != nil {
				onProgress("已收到 Images API 流式事件", int(time.Since(startedAt).Seconds()), rawBytes)
			}
		}
		if err := scanner.Err(); err != nil {
			if result, ok := extractor.result(); ok && result.ImageB64 != "" {
				return result, nil
			}
			return ImageResult{}, fmt.Errorf("read Images API stream: %w", err)
		}
		if resp.StatusCode/100 != 2 {
			return ImageResult{}, fmt.Errorf("上游返回 HTTP %d", resp.StatusCode)
		}
		if result, ok := extractor.result(); ok {
			return result, nil
		}
		return ImageResult{}, ErrNoImageInResponse
	}

	preview := newCappedPreviewBuffer(4096)
	teeReader := io.TeeReader(resp.Body, io.MultiWriter(rawSink, preview))

	var parsed imagesAPIResponse
	dec := json.NewDecoder(teeReader)
	if err := dec.Decode(&parsed); err != nil {
		_, _ = io.Copy(io.MultiWriter(rawSink, preview), resp.Body)
		bodyPreview := preview.String()
		if len(bodyPreview) > 400 {
			bodyPreview = bodyPreview[:400] + "..."
		}
		if resp.StatusCode/100 != 2 {
			return ImageResult{}, fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, bodyPreview)
		}
		return ImageResult{}, fmt.Errorf("解析 Images API 响应失败:%w", err)
	}

	// Non-2xx with JSON body — decode has already captured the structured error.
	if resp.StatusCode/100 != 2 {
		if parsed.Error != nil {
			return ImageResult{}, fmt.Errorf("上游返回 %d:%s", resp.StatusCode, parsed.Error.Message)
		}
		bodyPreview := preview.String()
		if len(bodyPreview) > 400 {
			bodyPreview = bodyPreview[:400] + "..."
		}
		return ImageResult{}, fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, bodyPreview)
	}
	if parsed.Error != nil {
		return ImageResult{}, fmt.Errorf("上游返回错误:%s", parsed.Error.Message)
	}
	return resolveImagesAPIResponse(ctx, httpClient, baseURL, opts.APIKey, parsed, useAsyncPolling, rawSink, onProgress, startedAt)
}

func imageResultFromImagesDatum(d imagesAPIDatum) ImageResult {
	return ImageResult{
		ImageB64:      d.B64JSON,
		RevisedPrompt: d.RevisedPrompt,
		SourceEvent:   "images_api",
	}
}

func appendImagesAsyncQuery(raw string, enabled bool) string {
	if !enabled {
		return raw
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("async", "true")
	u.RawQuery = q.Encode()
	return u.String()
}

func (r imagesAPIResponse) taskIdentifier() string {
	if strings.TrimSpace(r.TaskID) != "" {
		return strings.TrimSpace(r.TaskID)
	}
	return strings.TrimSpace(r.ID)
}

func (r imagesAPIResponse) firstDatum() (imagesAPIDatum, bool) {
	if len(r.Data) > 0 {
		return r.Data[0], true
	}
	if r.Detail != nil {
		if d, ok := r.Detail.firstDatum(); ok {
			return d, true
		}
	}
	if r.Result != nil {
		return r.Result.firstDatum()
	}
	return imagesAPIDatum{}, false
}

func (r imagesAPIResponse) errorMessage() string {
	if r.Error != nil && strings.TrimSpace(r.Error.Message) != "" {
		return strings.TrimSpace(r.Error.Message)
	}
	if r.Detail != nil {
		if msg := r.Detail.errorMessage(); msg != "" {
			return msg
		}
	}
	if r.Result != nil {
		return r.Result.errorMessage()
	}
	return ""
}

func normalizeImagesTaskStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func isImagesTaskPending(status string) bool {
	switch normalizeImagesTaskStatus(status) {
	case "", "queued", "pending", "processing", "running", "in_progress", "submitted":
		return true
	default:
		return false
	}
}

func isImagesTaskComplete(status string) bool {
	switch normalizeImagesTaskStatus(status) {
	case "completed", "succeeded", "success", "done":
		return true
	default:
		return false
	}
}

func isImagesTaskFailed(status string) bool {
	switch normalizeImagesTaskStatus(status) {
	case "failed", "cancelled", "canceled", "expired", "error":
		return true
	default:
		return false
	}
}

func imageResultFromImagesAPIResponse(ctx context.Context, httpClient *http.Client, parsed imagesAPIResponse) (ImageResult, bool, error) {
	d, ok := parsed.firstDatum()
	if !ok {
		return ImageResult{}, false, nil
	}
	if d.B64JSON == "" {
		imageURL := strings.TrimSpace(d.DownloadURL)
		if imageURL == "" {
			imageURL = strings.TrimSpace(d.URL)
		}
		if imageURL != "" {
			result, err := downloadImagesAPIURL(ctx, httpClient, imageURL)
			if err != nil {
				return ImageResult{}, true, err
			}
			result.RevisedPrompt = d.RevisedPrompt
			return result, true, nil
		}
		return ImageResult{}, true, ErrNoImageInResponse
	}
	return imageResultFromImagesDatum(d), true, nil
}

func downloadImagesAPIURL(ctx context.Context, httpClient *http.Client, imageURL string) (ImageResult, error) {
	u, err := neturl.Parse(imageURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ImageResult{}, fmt.Errorf("上游返回的图片下载地址无效:%s", imageURL)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return ImageResult{}, fmt.Errorf("上游返回的图片下载地址协议不支持:%s", u.Scheme)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return ImageResult{}, err
	}
	req.Header.Set("Accept", "image/*,*/*")
	req.Header.Set("User-Agent", UserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		return ImageResult{}, fmt.Errorf("下载异步任务图片失败:%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return ImageResult{}, fmt.Errorf("下载异步任务图片返回 HTTP %d", resp.StatusCode)
	}
	const maxDownloadBytes = 64 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return ImageResult{}, fmt.Errorf("读取异步任务图片失败:%w", err)
	}
	if len(data) == 0 {
		return ImageResult{}, errors.New("下载异步任务图片为空")
	}
	if len(data) > maxDownloadBytes {
		return ImageResult{}, fmt.Errorf("下载异步任务图片超过限制:%dMB", maxDownloadBytes/(1024*1024))
	}
	return ImageResult{
		ImageB64:    base64.StdEncoding.EncodeToString(data),
		SourceEvent: "images_api",
	}, nil
}

func resolveImagesAPIResponse(
	ctx context.Context,
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	parsed imagesAPIResponse,
	allowAsync bool,
	rawSink io.Writer,
	onProgress func(stage string, elapsedSeconds int, bytesReceived int64),
	startedAt time.Time,
) (ImageResult, error) {
	if msg := parsed.errorMessage(); msg != "" {
		return ImageResult{}, fmt.Errorf("上游返回错误:%s", msg)
	}
	if result, found, err := imageResultFromImagesAPIResponse(ctx, httpClient, parsed); found || err != nil {
		return result, err
	}
	status := normalizeImagesTaskStatus(parsed.Status)
	if isImagesTaskFailed(status) {
		return ImageResult{}, fmt.Errorf("Images API 异步任务失败:%s", parsed.Status)
	}
	taskID := parsed.taskIdentifier()
	if !allowAsync || taskID == "" {
		return ImageResult{}, ErrNoImageInResponse
	}
	if onProgress != nil {
		stage := "Images API 已提交异步任务，等待结果"
		if isImagesTaskComplete(status) {
			stage = "Images API 异步任务已完成，等待图片结果写入"
		}
		onProgress(stage, int(time.Since(startedAt).Seconds()), 0)
	}
	return pollImagesTask(ctx, httpClient, baseURL, apiKey, taskID, rawSink, onProgress, startedAt)
}

func pollImagesTask(
	ctx context.Context,
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	taskID string,
	rawSink io.Writer,
	onProgress func(stage string, elapsedSeconds int, bytesReceived int64),
	startedAt time.Time,
) (ImageResult, error) {
	timeout := time.Duration(ImagesTaskPollTimeoutSecond) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	interval := time.Duration(ImagesTaskPollIntervalSecond) * time.Second
	completedResultGrace := time.Duration(ImagesTaskCompletedResultGraceSecond) * time.Second
	if completedResultGrace <= 0 {
		completedResultGrace = 2 * time.Minute
	}
	pollURL := imagesTaskPollURL(baseURL, taskID)
	var lastStatus string
	var completedWithoutResultSince time.Time
	for attempt := 1; ; attempt++ {
		if attempt > 1 && interval > 0 {
			if !sleepCtx(ctx, interval) {
				return ImageResult{}, ctx.Err()
			}
		}
		if time.Now().After(deadline) {
			if lastStatus == "" {
				return ImageResult{}, fmt.Errorf("Images API 异步任务轮询超时")
			}
			return ImageResult{}, fmt.Errorf("Images API 异步任务轮询超时,最后状态:%s", lastStatus)
		}
		if onProgress != nil {
			stage := "Images API 异步任务处理中"
			if lastStatus != "" {
				stage = "Images API 异步任务处理中:" + lastStatus
			}
			onProgress(stage, int(time.Since(startedAt).Seconds()), 0)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return ImageResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", UserAgent())
		resp, err := httpClient.Do(req)
		if err != nil {
			return ImageResult{}, err
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if writeErr := writeImagesPollRaw(rawSink, taskID, attempt, raw); writeErr != nil {
			return ImageResult{}, writeErr
		}
		if readErr != nil {
			return ImageResult{}, fmt.Errorf("读取 Images API 异步任务响应失败:%w", readErr)
		}
		var parsed imagesAPIResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			if resp.StatusCode/100 != 2 {
				return ImageResult{}, fmt.Errorf("上游轮询返回 HTTP %d: %s", resp.StatusCode, imagesRawPreview(raw))
			}
			return ImageResult{}, fmt.Errorf("解析 Images API 异步任务响应失败:%w", err)
		}
		if resp.StatusCode/100 != 2 {
			if msg := parsed.errorMessage(); msg != "" {
				return ImageResult{}, fmt.Errorf("上游轮询返回 %d:%s", resp.StatusCode, msg)
			}
			return ImageResult{}, fmt.Errorf("上游轮询返回 HTTP %d: %s", resp.StatusCode, imagesRawPreview(raw))
		}
		if msg := parsed.errorMessage(); msg != "" {
			return ImageResult{}, fmt.Errorf("上游轮询返回错误:%s", msg)
		}
		if result, found, err := imageResultFromImagesAPIResponse(ctx, httpClient, parsed); found || err != nil {
			return result, err
		}
		lastStatus = strings.TrimSpace(parsed.Status)
		if isImagesTaskFailed(lastStatus) {
			return ImageResult{}, fmt.Errorf("Images API 异步任务失败:%s", lastStatus)
		}
		if isImagesTaskComplete(lastStatus) {
			if completedWithoutResultSince.IsZero() {
				completedWithoutResultSince = time.Now()
			}
			if time.Since(completedWithoutResultSince) <= completedResultGrace {
				if onProgress != nil {
					onProgress("Images API 异步任务已完成，等待图片结果写入", int(time.Since(startedAt).Seconds()), 0)
				}
				continue
			}
			return ImageResult{}, ErrNoImageInResponse
		}
		completedWithoutResultSince = time.Time{}
		if !isImagesTaskPending(lastStatus) {
			return ImageResult{}, fmt.Errorf("Images API 异步任务状态未知:%s", lastStatus)
		}
	}
}

func imagesTaskPollURL(baseURL string, taskID string) string {
	u := baseURL + "/v1/images/" + neturl.PathEscape(taskID)
	parsed, err := neturl.Parse(u)
	if err != nil {
		return u + "?detail=true"
	}
	q := parsed.Query()
	q.Set("detail", "true")
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func writeImagesPollRaw(rawSink io.Writer, taskID string, attempt int, raw []byte) error {
	if rawSink == nil {
		return nil
	}
	if _, err := fmt.Fprintf(rawSink, "\n\n--- images-task-%s-poll-%d ---\n", taskID, attempt); err != nil {
		return fmt.Errorf("write raw: %w", err)
	}
	if _, err := rawSink.Write(raw); err != nil {
		return fmt.Errorf("write raw: %w", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		if _, err := rawSink.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write raw: %w", err)
		}
	}
	return nil
}

func imagesRawPreview(raw []byte) string {
	if len(raw) > 400 {
		raw = raw[:400]
	}
	return string(raw)
}

func parseImagesAPIResponseBytes(raw []byte, statusCode int) (ImageResult, error) {
	var parsed imagesAPIResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ImageResult{}, err
	}
	if statusCode/100 != 2 {
		if parsed.Error != nil {
			return ImageResult{}, fmt.Errorf("上游返回 %d:%s", statusCode, parsed.Error.Message)
		}
		return ImageResult{}, fmt.Errorf("上游返回 HTTP %d", statusCode)
	}
	if parsed.Error != nil {
		return ImageResult{}, fmt.Errorf("上游返回错误:%s", parsed.Error.Message)
	}
	if result, found, err := imageResultFromImagesAPIResponse(context.Background(), http.DefaultClient, parsed); found || err != nil {
		return result, err
	}
	return ImageResult{}, ErrNoImageInResponse
}

type cappedPreviewBuffer struct {
	buf []byte
	max int
}

func newCappedPreviewBuffer(max int) *cappedPreviewBuffer {
	return &cappedPreviewBuffer{max: max}
}

func (b *cappedPreviewBuffer) Write(p []byte) (int, error) {
	if len(b.buf) < b.max {
		remain := b.max - len(b.buf)
		if len(p) < remain {
			remain = len(p)
		}
		b.buf = append(b.buf, p[:remain]...)
	}
	return len(p), nil
}

func (b *cappedPreviewBuffer) String() string {
	return string(b.buf)
}

// imageSourcePathsForEdit picks the source-image paths for an Images API edit.
// Prefers ImagePaths (raw files, no decode needed). If only data URLs are
// provided, the caller is responsible for writing them to a temp file first
// — see writeDataURLToTemp below.
func (o Options) imageSourcePathsForEdit() []string {
	paths := make([]string, 0, len(o.ImagePaths)+1)
	for _, p := range o.ImagePaths {
		if strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) > 0 {
		return paths
	}
	// Fallback: data URLs → temp files.
	for _, du := range o.EffectiveImageDataURLs() {
		if p, err := writeDataURLToTemp(du); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// writeDataURLToTemp materialises a `data:...;base64,...` URL to a temp file
// and returns its path. Caller is responsible for cleanup; we leave it for the
// OS temp sweeper since these are small and we want them to survive retries.
func writeDataURLToTemp(dataURL string) (string, error) {
	idx := strings.Index(dataURL, ",")
	if !strings.HasPrefix(dataURL, "data:") || idx < 0 {
		return "", errors.New("not a data URL")
	}
	header := dataURL[5:idx] // e.g. "image/png;base64"
	payload := dataURL[idx+1:]
	if !strings.Contains(header, "base64") {
		return "", errors.New("data URL not base64")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	ext := ".png"
	if strings.HasPrefix(header, "image/jpeg") {
		ext = ".jpg"
	} else if strings.HasPrefix(header, "image/webp") {
		ext = ".webp"
	}
	f, err := os.CreateTemp("", "image-studio-edit-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// buildEditsMultipart constructs the multipart/form-data body for /v1/images/edits.
// 多张源图按 image[] / image[1] / ... 形式串联 —— 不同中转站对多图编辑支持不一,
// 仅第一张是 OpenAI 官方接受的最小可用形态,其余作为兼容性 best-effort。
func buildEditsMultipart(
	paths []string, maskB64, prompt, model, size, quality, outputFormat, background string, outputCompression int, inputFidelity, moderation, userIdentifier, negativePrompt string, seed int64, requestPolicy RequestPolicy, partialImages int, useNewAPICompat bool, useAsyncPolling bool,
) (*bytes.Buffer, string, error) {
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)

	for i, p := range paths {
		fieldName := "image"
		if i > 0 {
			// Some relays accept multiple `image` fields, others want image[] —
			// we send both to maximise compatibility. The extra field is cheap.
			fieldName = "image[]"
		}
		if err := writeMultipartFile(w, fieldName, p); err != nil {
			return nil, "", fmt.Errorf("attach %s: %w", filepath.Base(p), err)
		}
	}

	if strings.TrimSpace(maskB64) != "" {
		raw, err := base64.StdEncoding.DecodeString(maskB64)
		if err == nil && len(raw) > 0 {
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", `form-data; name="mask"; filename="mask.png"`)
			h.Set("Content-Type", "image/png")
			fw, err := w.CreatePart(h)
			if err != nil {
				return nil, "", err
			}
			if _, err := fw.Write(raw); err != nil {
				return nil, "", err
			}
		}
	}

	_ = w.WriteField("prompt", prompt)
	_ = w.WriteField("model", model)
	_ = w.WriteField("n", "1")
	_ = w.WriteField("size", size)
	_ = w.WriteField("quality", quality)
	if strings.TrimSpace(outputFormat) != "" {
		_ = w.WriteField("output_format", outputFormat)
	}
	if supportsImageBackground(model) {
		_ = w.WriteField("background", background)
	}
	if supportsOutputCompression(model, outputFormat) {
		_ = w.WriteField("output_compression", fmt.Sprintf("%d", outputCompression))
	}
	if supportsInputFidelity(model) && inputFidelity != DefaultInputFidelity {
		_ = w.WriteField("input_fidelity", inputFidelity)
	}
	if supportsImageModeration(model) {
		_ = w.WriteField("moderation", moderation)
	}
	if userIdentifier != "" {
		_ = w.WriteField("user", userIdentifier)
	}
	if useAsyncPolling {
		_ = w.WriteField("async", "true")
	}
	if useNewAPICompat || useAsyncPolling || supportsImagesResponseFormat(model, ModeEdit) {
		_ = w.WriteField("response_format", "b64_json")
	}
	if !useNewAPICompat && !useAsyncPolling {
		_ = w.WriteField("stream", "true")
		_ = w.WriteField("partial_images", fmt.Sprintf("%d", partialImages))
	}
	if shouldSendExtendedImageParameters(requestPolicy) && seed != 0 {
		_ = w.WriteField("seed", fmt.Sprintf("%d", seed))
	}
	if shouldSendExtendedImageParameters(requestPolicy) && strings.TrimSpace(negativePrompt) != "" {
		_ = w.WriteField("negative_prompt", negativePrompt)
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf, w.FormDataContentType(), nil
}

func writeMultipartFile(w *multipart.Writer, fieldName, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > MaxInputImageBytes {
		return fmt.Errorf("源图过大(%dB > %dB 上限)", st.Size(), MaxInputImageBytes)
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filepath.Base(path)))
	h.Set("Content-Type", mimeForPath(path))
	fw, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, f)
	return err
}

func mimeForPath(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	if m, ok := SupportedImageMime[ext]; ok {
		return m
	}
	return "application/octet-stream"
}
