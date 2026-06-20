package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	gioCompat "image-studio/gio-client/internal/compat"
	sharedCompat "image-studio/shared/compat"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
)

func nextPresetName(items []sharedCompat.Preset) string {
	used := map[int]struct{}{}
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if !strings.HasPrefix(name, "配置") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(name, "配置"))
		value, err := strconv.Atoi(raw)
		if err == nil && value > 0 {
			used[value] = struct{}{}
		}
	}
	for i := 1; ; i++ {
		if _, ok := used[i]; !ok {
			return "配置" + strconv.Itoa(i)
		}
	}
}

func (a *App) presetListButton(id string) *widget.Clickable {
	if a.presetListButtons == nil {
		a.presetListButtons = map[string]*widget.Clickable{}
	}
	if btn, ok := a.presetListButtons[id]; ok {
		return btn
	}
	btn := new(widget.Clickable)
	a.presetListButtons[id] = btn
	return btn
}

func (a *App) currentPresetSnapshot() sharedCompat.Preset {
	compression := 100
	if raw := strings.TrimSpace(a.outputCompressionInput.Text()); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			compression = value
		}
	}
	return sharedCompat.Preset{
		Name:              strings.TrimSpace(a.presetNameInput.Text()),
		Size:              strings.TrimSpace(a.size),
		Quality:           strings.TrimSpace(a.quality),
		OutputFormat:      strings.TrimSpace(a.format),
		NegativePrompt:    strings.TrimSpace(a.negativePromptInput.Text()),
		Background:        strings.TrimSpace(a.background),
		OutputCompression: &compression,
		InputFidelity:     strings.TrimSpace(a.inputFidelity),
		ImageStyle:        strings.TrimSpace(a.imageStyle),
		Moderation:        strings.TrimSpace(a.moderation),
		StyleTag:          strings.TrimSpace(a.styleTag),
		KernelRuntimeMode: normalizeKernelRuntimeMode(a.kernelRuntimeMode),
		BatchCount:        normalizeBatchCount(a.batchCount),
	}
}

func (a *App) openPresetManager() {
	a.mu.Lock()
	if a.selectedPresetID == "" && len(a.presets) > 0 {
		a.selectedPresetID = strings.TrimSpace(a.presets[0].ID)
	}
	if a.selectedPresetID != "" {
		a.loadPresetDraftLocked(a.selectedPresetID)
	} else {
		a.presetNameInput.SetText(nextPresetName(a.presets))
	}
	a.presetManagerOpen = true
	a.mu.Unlock()
	a.invalidateNow()
}

func (a *App) closePresetManager() {
	a.mu.Lock()
	a.presetManagerOpen = false
	a.mu.Unlock()
	a.invalidateNow()
}

func (a *App) loadPresetDraftLocked(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, item := range a.presets {
		if strings.TrimSpace(item.ID) != id {
			continue
		}
		a.selectedPresetID = id
		a.presetNameInput.SetText(strings.TrimSpace(item.Name))
		return true
	}
	return false
}

func (a *App) startNewPresetDraft() {
	a.mu.Lock()
	a.selectedPresetID = ""
	a.presetNameInput.SetText(nextPresetName(a.presets))
	a.mu.Unlock()
	a.invalidateNow()
}

func (a *App) savePresetAsNew() {
	name := strings.TrimSpace(a.presetNameInput.Text())
	if name == "" {
		a.appendLog("预设名称不能为空")
		return
	}
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("保存预设失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	now := time.Now().UnixMilli()
	preset := a.currentPresetSnapshot()
	preset.ID = fmt.Sprintf("preset-%d", now)
	preset.Name = name
	state.Settings.Presets = append([]sharedCompat.Preset{preset}, state.Settings.Presets...)
	state.UpdatedAt = now
	if err := gioCompat.SaveState(state); err != nil {
		a.appendLog("保存预设失败: " + err.Error())
		return
	}
	a.mu.Lock()
	a.setPresetsLocked(state.Settings.Presets)
	a.selectedPresetID = preset.ID
	a.presetNameInput.SetText(preset.Name)
	a.mu.Unlock()
	a.appendLog("已保存预设: " + preset.Name)
	a.invalidateNow()
}

func (a *App) overwriteSelectedPreset() {
	targetID := ""
	a.mu.Lock()
	targetID = strings.TrimSpace(a.selectedPresetID)
	a.mu.Unlock()
	if targetID == "" {
		a.appendLog("当前没有可覆盖的预设")
		return
	}
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("更新预设失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	snapshot := a.currentPresetSnapshot()
	updated := false
	name := ""
	for i := range state.Settings.Presets {
		if strings.TrimSpace(state.Settings.Presets[i].ID) != targetID {
			continue
		}
		name = strings.TrimSpace(state.Settings.Presets[i].Name)
		snapshot.ID = state.Settings.Presets[i].ID
		snapshot.Name = name
		state.Settings.Presets[i] = snapshot
		updated = true
		break
	}
	if !updated {
		a.appendLog("当前预设不存在")
		return
	}
	state.UpdatedAt = time.Now().UnixMilli()
	if err := gioCompat.SaveState(state); err != nil {
		a.appendLog("更新预设失败: " + err.Error())
		return
	}
	a.mu.Lock()
	a.setPresetsLocked(state.Settings.Presets)
	a.selectedPresetID = targetID
	a.presetNameInput.SetText(name)
	a.mu.Unlock()
	a.appendLog("已更新预设: " + name)
	a.invalidateNow()
}

func (a *App) deleteSelectedPreset() {
	targetID := ""
	a.mu.Lock()
	targetID = strings.TrimSpace(a.selectedPresetID)
	a.mu.Unlock()
	if targetID == "" {
		a.appendLog("当前没有可删除的预设")
		return
	}
	state, _, err := gioCompat.LoadState()
	if err != nil {
		a.appendLog("删除预设失败: " + err.Error())
		return
	}
	state = sharedCompat.Normalize(state)
	next := make([]sharedCompat.Preset, 0, len(state.Settings.Presets))
	removedName := ""
	for _, item := range state.Settings.Presets {
		if strings.TrimSpace(item.ID) == targetID {
			removedName = strings.TrimSpace(item.Name)
			continue
		}
		next = append(next, item)
	}
	if len(next) == len(state.Settings.Presets) {
		a.appendLog("当前预设不存在")
		return
	}
	state.Settings.Presets = next
	state.UpdatedAt = time.Now().UnixMilli()
	if err := gioCompat.SaveState(state); err != nil {
		a.appendLog("删除预设失败: " + err.Error())
		return
	}
	a.mu.Lock()
	a.setPresetsLocked(state.Settings.Presets)
	if len(state.Settings.Presets) > 0 {
		a.selectedPresetID = strings.TrimSpace(state.Settings.Presets[0].ID)
		a.presetNameInput.SetText(strings.TrimSpace(state.Settings.Presets[0].Name))
	} else {
		a.selectedPresetID = ""
		a.presetNameInput.SetText(nextPresetName(nil))
	}
	a.mu.Unlock()
	if removedName == "" {
		removedName = targetID
	}
	a.appendLog("已删除预设: " + removedName)
	a.invalidateNow()
}

func (a *App) applySelectedPreset() {
	targetID := ""
	a.mu.Lock()
	targetID = strings.TrimSpace(a.selectedPresetID)
	a.mu.Unlock()
	if !a.applyPresetByID(targetID) {
		a.appendLog("当前预设不存在")
	}
}

func (a *App) layoutPresetManagerModal(gtx layout.Context, snap snapshot) layout.Dimensions {
	for a.closePresetManagerButton.Clicked(gtx) {
		a.closePresetManager()
	}
	for a.newPresetButton.Clicked(gtx) {
		a.startNewPresetDraft()
	}
	for a.savePresetButton.Clicked(gtx) {
		a.savePresetAsNew()
	}
	for a.overwritePresetButton.Clicked(gtx) {
		a.overwriteSelectedPreset()
	}
	for a.applyPresetButton.Clicked(gtx) {
		a.applySelectedPreset()
	}
	for a.deletePresetButton.Clicked(gtx) {
		a.deleteSelectedPreset()
	}
	for _, item := range snap.Presets {
		btn := a.presetListButton("preset-list:" + item.ID)
		for btn.Clicked(gtx) {
			a.mu.Lock()
			a.loadPresetDraftLocked(item.ID)
			a.mu.Unlock()
			a.invalidateNow()
		}
	}

	return a.layoutStandardModal(
		gtx,
		unit.Dp(900),
		unit.Dp(620),
		"参数预设",
		"",
		&a.closePresetManagerButton,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(unit.Dp(12))}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return fixedWidth(gtx, unit.Dp(280), func(gtx layout.Context) layout.Dimensions {
						return a.borderedSurface(gtx, fluent.surface, fluentCardRadius, fluent.border, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								children := []layout.FlexChild{
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(unit.Dp(8))}.Layout(gtx,
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return a.compactIconTextButton(gtx, &a.newPresetButton, uiIconAdd, "新建", false)
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return a.compactIconTextButton(gtx, &a.applyPresetButton, uiIconCheck, "应用", true)
											}),
										)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
								}
								if len(snap.Presets) == 0 {
									children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.label(gtx, "还没有保存参数预设。先调好当前参数，再保存一条。", unit.Sp(11), fluent.textDim, font.Normal)
									}))
								} else {
									items := a.presetLabelsCached(snap.Presets)
									for idx, item := range snap.Presets {
										item := item
										summary := items[idx]
										children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											selected := strings.TrimSpace(item.ID) == strings.TrimSpace(a.selectedPresetID)
											return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return a.surfaceButton(
													gtx,
													a.presetListButton("preset-list:"+item.ID),
													chooseColor(selected, fluent.accentSoft, rgba(0xffffff, 0x00)),
													chooseColor(selected, accentAlpha(0x18), fluent.surface2),
													chooseColor(selected, accentAlpha(0x38), fluent.border),
													fluentCardRadius,
													layout.Inset{Top: 9, Bottom: 9, Left: 10, Right: 10},
													func(gtx layout.Context) layout.Dimensions {
														return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(unit.Dp(3))}.Layout(gtx,
															layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																return a.singleLineLabel(gtx, strings.TrimSpace(summary.Title), unit.Sp(11), chooseColor(selected, fluent.accent, fluent.text), font.SemiBold)
															}),
															layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																return a.singleLineLabel(gtx, strings.TrimSpace(summary.Detail), unit.Sp(10), fluent.textDim, font.Normal)
															}),
														)
													},
												)
											})
										}))
									}
								}
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
							})
						})
					})
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return a.borderedSurface(gtx, fluent.surface, fluentCardRadius, fluent.border, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(unit.Dp(10))}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									title := "保存当前参数"
									if strings.TrimSpace(a.selectedPresetID) != "" {
										title = "当前选中预设"
									}
									return a.label(gtx, title, unit.Sp(12), fluent.text, font.SemiBold)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.field(gtx, "预设名称", &a.presetNameInput, "配置1", unit.Dp(42))
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									summary := a.currentPresetSnapshot()
									lines := compactNonEmpty([]string{
										sizeDisplayLabel(summary.Size),
										qualityDisplayLabel(summary.Quality),
										strings.ToUpper(strings.TrimSpace(summary.OutputFormat)),
										"负向: " + chooseValueOrFallback(summary.NegativePrompt, "空"),
										"背景: " + chooseValueOrFallback(summary.Background, "auto"),
										"保真: " + chooseValueOrFallback(summary.InputFidelity, "auto"),
										"图风: " + chooseValueOrFallback(summary.ImageStyle, "default"),
										"审核: " + chooseValueOrFallback(summary.Moderation, "low"),
										"风格: " + chooseValueOrFallback(summary.StyleTag, "默认"),
										fmt.Sprintf("张数: %d", summary.BatchCount),
									})
									rows := make([]layout.FlexChild, 0, len(lines))
									for _, line := range lines {
										line := line
										rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return a.label(gtx, line, unit.Sp(10), fluent.textDim, font.Normal)
										}))
									}
									return a.borderedSurface(gtx, fluent.surface2, fluentCardRadius, fluent.border, func(gtx layout.Context) layout.Dimensions {
										return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(unit.Dp(4))}.Layout(gtx, rows...)
										})
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(unit.Dp(8))}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return a.compactIconTextButton(gtx, &a.savePresetButton, uiIconSave, "另存新预设", false)
										}),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return a.compactIconTextButton(gtx, &a.overwritePresetButton, uiIconRefresh, "覆盖当前预设", true)
										}),
									)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(unit.Dp(8))}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return a.compactIconTextButton(gtx, &a.applyPresetButton, uiIconCheck, "应用到当前参数", true)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return fixedWidth(gtx, unit.Dp(124), func(gtx layout.Context) layout.Dimensions {
												return a.compactIconTextButton(gtx, &a.deletePresetButton, uiIconDelete, "删除", false)
											})
										}),
									)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.label(gtx, fmt.Sprintf("当前已保存 %d 条共享预设，WebView 与 Gio 会共用这份状态。", len(snap.Presets)), unit.Sp(10), fluent.textDim, font.Normal)
								}),
							)
						})
					})
				}),
			)
		},
	)
}

func chooseValueOrFallback(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
