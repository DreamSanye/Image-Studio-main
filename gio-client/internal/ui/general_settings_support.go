package ui

import (
	"encoding/json"
	"fmt"
	"image-studio/gio-client/internal/kernel"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuanhua/image-gptcodex/pkg/client"
	gioCompat "image-studio/gio-client/internal/compat"
	sharedCompat "image-studio/shared/compat"
)

var generalKernelRuntimeChoices = []settingsOptionChoice{
	{Title: "auto(按宿主自动选择)", Detail: "按宿主自动选择", Value: "auto"},
	{Title: "local(桌面 Go/Wails)", Detail: "桌面 Go/Wails", Value: "local"},
	{Title: "remote(共享远程内核)", Detail: "共享远程内核", Value: "remote"},
}

var generalAutoRetryCountChoices = []int{1, 3, 5, 8, 10}

const (
	defaultLoopGenerationCount       = 10
	defaultLoopGenerationConcurrency = 2
	maxLoopGenerationCount           = 99
	maxLoopGenerationConcurrency     = 9
)

type historyExportPayload struct {
	Version    int                        `json:"version"`
	ExportedAt string                     `json:"exportedAt"`
	Count      int                        `json:"count"`
	Items      []sharedCompat.HistoryItem `json:"items"`
}

func normalizeKernelRuntimeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "auto", "local", "remote":
		return strings.TrimSpace(mode)
	default:
		return "auto"
	}
}

func kernelRuntimeModeLabel(mode string) string {
	mode = normalizeKernelRuntimeMode(mode)
	for _, choice := range generalKernelRuntimeChoices {
		if choice.Value == mode {
			return choice.Title
		}
	}
	return generalKernelRuntimeChoices[0].Title
}

func normalizeAutoRetryCount(value int) int {
	if value <= 0 {
		return client.DefaultAutoRetryCount
	}
	if value > client.MaxAutoRetryCount {
		return client.MaxAutoRetryCount
	}
	return value
}

func normalizeLoopGenerationCount(value int) int {
	if value <= 0 {
		return defaultLoopGenerationCount
	}
	if value > maxLoopGenerationCount {
		return maxLoopGenerationCount
	}
	return value
}

func normalizeLoopGenerationConcurrency(value int) int {
	if value <= 0 {
		return defaultLoopGenerationConcurrency
	}
	if value > maxLoopGenerationConcurrency {
		return maxLoopGenerationConcurrency
	}
	return value
}

func normaliseCompletionSoundSettings(value *sharedCompat.CompletionSoundSettings) sharedCompat.CompletionSoundSettings {
	return gioCompat.NormaliseCompletionSoundSettings(value)
}

func normaliseCompletionNotificationSettings(value *sharedCompat.CompletionNotificationSettings) sharedCompat.CompletionNotificationSettings {
	return gioCompat.NormaliseCompletionNotificationSettings(value)
}

func (a *App) exportHistoryJSON() {
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("导出历史失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	if len(state.History) == 0 {
		a.appendLog("没有可导出的历史记录")
		return
	}
	filename := fmt.Sprintf("image-studio-history-%s.json", time.Now().Format("20060102-150405"))
	dst, err := chooseSaveJSONFile(filename)
	if err != nil {
		a.appendLog("选择导出文件失败: " + err.Error())
		return
	}
	if strings.TrimSpace(dst) == "" {
		return
	}
	payload := historyExportPayload{
		Version:    1,
		ExportedAt: time.Now().Format(time.RFC3339),
		Count:      len(state.History),
		Items:      append([]sharedCompat.HistoryItem(nil), state.History...),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		a.appendLog("导出历史失败: " + err.Error())
		return
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		a.appendLog("写入历史导出文件失败: " + err.Error())
		return
	}
	a.appendLog(fmt.Sprintf("已导出 %d 条历史: %s", len(state.History), filepath.Base(dst)))
}

func (a *App) importHistoryJSON() {
	src, err := chooseJSONFile()
	if err != nil {
		a.appendLog("选择历史文件失败: " + err.Error())
		return
	}
	if strings.TrimSpace(src) == "" {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		a.appendLog("读取历史文件失败: " + err.Error())
		return
	}
	incoming, err := parseImportedHistoryItems(data)
	if err != nil {
		a.appendLog("导入历史失败: " + err.Error())
		return
	}
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("导入历史失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	existing := make(map[string]struct{}, len(state.History))
	for _, item := range state.History {
		existing[strings.TrimSpace(item.ID)] = struct{}{}
	}
	added := 0
	for _, item := range incoming {
		item = normalizeImportedHistoryItem(item)
		if item.ID == "" || item.CreatedAt == 0 {
			continue
		}
		if _, ok := existing[item.ID]; ok {
			continue
		}
		existing[item.ID] = struct{}{}
		state.History = append(state.History, item)
		added++
	}
	if added == 0 {
		a.appendLog("导入完成，但没有新增历史项")
		return
	}
	sort.Slice(state.History, func(i, j int) bool {
		return state.History[i].CreatedAt > state.History[j].CreatedAt
	})
	state.UpdatedAt = time.Now().UnixMilli()
	if err := gioCompat.SaveState(state); err != nil {
		a.appendLog("保存导入后的历史失败: " + err.Error())
		return
	}
	a.mu.Lock()
	a.setHistoryLocked(state.History)
	a.mu.Unlock()
	if latest, ok := newestHistoryItem(state.History); ok {
		if err := a.loadHistoryPreview(latest, false); err != nil && !isMissingPreview(err) {
			a.appendLog("载入导入后的最近历史失败: " + err.Error())
		}
	}
	a.appendLog(fmt.Sprintf("已导入 %d 条历史: %s", added, filepath.Base(src)))
}

func (a *App) clearCurrentProfileAPIKey() {
	profileID := strings.TrimSpace(a.activeProfileID)
	if profileID == "" {
		a.appendLog("当前没有可清除 API Key 的活动配置")
		return
	}
	if err := gioCompat.WriteAPIKey(profileID, ""); err != nil {
		a.appendLog("清除 API Key 失败: " + err.Error())
		return
	}
	a.apiKeyInput.SetText("")
	a.appendLog("已清除当前活动配置的 API Key")
}

func (a *App) clearAllHistory() {
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("清空历史失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	if len(state.History) == 0 {
		a.appendLog("当前没有历史记录可清空")
		return
	}
	state.History = nil
	state.UpdatedAt = time.Now().UnixMilli()
	if err := gioCompat.SaveState(state); err != nil {
		a.appendLog("清空历史失败: " + err.Error())
		return
	}
	a.replaceHistoryState(nil, "已清空全部历史记录")
}

func (a *App) pruneHistoryOlderThanDays(days int) {
	if days <= 0 {
		return
	}
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("清理历史失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	if len(state.History) == 0 {
		a.appendLog("当前没有历史记录可清理")
		return
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	next := make([]sharedCompat.HistoryItem, 0, len(state.History))
	removed := 0
	for _, item := range state.History {
		if item.CreatedAt > 0 && item.CreatedAt < cutoff {
			removed++
			continue
		}
		next = append(next, item)
	}
	if removed == 0 {
		a.appendLog(fmt.Sprintf("没有 %d 天前的历史需要清理", days))
		return
	}
	state.History = next
	state.UpdatedAt = time.Now().UnixMilli()
	if err := gioCompat.SaveState(state); err != nil {
		a.appendLog("清理历史失败: " + err.Error())
		return
	}
	a.replaceHistoryState(next, fmt.Sprintf("已清理 %d 条 %d 天前的历史", removed, days))
}

func (a *App) replaceHistoryState(next []sharedCompat.HistoryItem, logMessage string) {
	kept := make(map[string]struct{}, len(next))
	for _, item := range next {
		kept[item.ID] = struct{}{}
	}
	a.mu.Lock()
	a.setHistoryLocked(next)
	if len(a.batchResultIDs) > 0 {
		filtered := make([]string, 0, len(a.batchResultIDs))
		for _, id := range a.batchResultIDs {
			if _, ok := kept[id]; ok {
				filtered = append(filtered, id)
			}
		}
		a.batchResultIDs = filtered
		if !a.canOpenResultGridLocked() {
			a.resultGridOpen = false
		}
	}
	if _, ok := kept[a.selectedHistoryID]; !ok {
		a.selectedHistoryID = ""
	}
	if a.compare.Item.ID != "" {
		if _, ok := kept[a.compare.Item.ID]; !ok {
			a.compare = resultState{Rev: a.compare.Rev + 1}
			a.compareSplitSlider.Value = 0.5
		}
	}
	if a.activeResultDetail.ID != "" {
		if _, ok := kept[a.activeResultDetail.ID]; !ok {
			a.activeResultDetail = sharedCompat.HistoryItem{}
		}
	}
	if a.result.Item.ID != "" {
		if _, ok := kept[a.result.Item.ID]; !ok {
			a.result = resultState{Rev: a.result.Rev + 1}
		}
	}
	if a.activePromptGroup.Key != "" {
		found := false
		for _, item := range a.activePromptGroup.Items {
			if item != nil {
				if _, ok := kept[item.ID]; ok {
					found = true
					break
				}
			}
		}
		if !found {
			a.activePromptGroup = historyPromptGroup{}
		}
	}
	if strings.TrimSpace(logMessage) != "" {
		a.appendLogLocked(logMessage)
	}
	a.mu.Unlock()
	a.invalidateNow()
}

func parseImportedHistoryItems(data []byte) ([]sharedCompat.HistoryItem, error) {
	var payload historyExportPayload
	if err := json.Unmarshal(data, &payload); err == nil && len(payload.Items) > 0 {
		return payload.Items, nil
	}
	var items []sharedCompat.HistoryItem
	if err := json.Unmarshal(data, &items); err == nil && len(items) > 0 {
		return items, nil
	}
	return nil, fmt.Errorf("文件里没有可导入的历史记录")
}

func normalizeImportedHistoryItem(item sharedCompat.HistoryItem) sharedCompat.HistoryItem {
	item.ID = strings.TrimSpace(item.ID)
	item.Prompt = strings.TrimSpace(item.Prompt)
	item.RevisedPrompt = strings.TrimSpace(item.RevisedPrompt)
	item.Mode = strings.TrimSpace(item.Mode)
	item.Size = strings.TrimSpace(item.Size)
	item.Quality = strings.TrimSpace(item.Quality)
	item.OutputFormat = strings.TrimSpace(item.OutputFormat)
	item.PreviewPath = strings.TrimSpace(item.PreviewPath)
	item.SavedPath = strings.TrimSpace(item.SavedPath)
	item.ThumbPath = strings.TrimSpace(item.ThumbPath)
	if len(item.SourcePaths) > 0 {
		item.SourcePaths = kernel.ParseSourcePaths(strings.Join(item.SourcePaths, "\n"))
	}
	if item.SavedPath == "" && item.ThumbPath != "" {
		item.SavedPath = item.ThumbPath
	}
	return item
}
