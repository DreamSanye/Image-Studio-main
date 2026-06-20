# Issue 关单评论模板

更新时间：2026-06-07

本文档只覆盖当前 **GitHub 仍 open，但当前仓库代码与本地验证已经覆盖** 的 issue。

适用范围：

- `#24`
- `#25`
- `#26`
- `#27`
- `#28`
- `#32`
- `#35`
- `#37`
- `#40`
- `#42`

统一验证基线：

- 2026-06-07 已重新跑通本地平台总链：
  - `IMAGE_STUDIO_INCLUDE_ISSUE_CLOSE_VERIFY=1 IMAGE_STUDIO_VERIFY_RESULTS_DIR=/private/tmp/verify-results-issue-close-final4 node scripts/verify-local-platform-kernel.mjs`
  - `platform-kernel-summary.json`：`status = passed`
  - 结构化结果：
    - `/private/tmp/verify-results-issue-close-final4/platform-kernel-summary.json`
    - `/private/tmp/verify-results-issue-close-final4/live-verify.json`
    - `/private/tmp/verify-results-issue-close-final4/local-smoke.json`
    - `/private/tmp/verify-results-issue-close-final4/android-shell.json`
    - `/private/tmp/verify-results-issue-close-final4/macos-release.json`
    - `/private/tmp/verify-results-issue-close-final4/issue-close-tooling.json`
    - `/private/tmp/verify-results-issue-close-final4/issue-close-export-bundle/manifest.json`
- 前端测试当前为 `114/114` 通过。

建议用法：

1. 先确认对应功能没有被后续改坏。
2. 复制下面对应 issue 的评论模板。
3. 如仓库维护方式允许，评论后可直接关闭 issue。

也可以直接使用本地 helper：

```bash
node scripts/issue-close-helper.mjs list
node scripts/issue-close-helper.mjs comment 24
node scripts/issue-close-helper.mjs plan
node scripts/issue-close-helper.mjs verify-open
node scripts/issue-close-helper.mjs export .tmp/issue-close-export/manual-check
node scripts/render-issue-close-comments.mjs --write
```

`export` 产物当前会包含：

- `manifest.json`
- `README.md`
- `plan.json`
- `plan.md`
- `issue-24.md` ... `issue-42.md`

如果只是想先确认“当前哪些 issue 会被处理、处理方式是什么”，优先用：

```bash
node scripts/issue-close-helper.mjs plan
node scripts/issue-close-helper.mjs plan 24 25 --comment-only
```

`apply` 默认不会执行。只有显式提供 `--execute`，并且环境里存在 `GITHUB_TOKEN` 或 `GH_TOKEN` 时，才会真正对 GitHub 发评论或关闭 issue。例如：

```bash
node scripts/issue-close-helper.mjs apply 24 --comment-only --execute
node scripts/issue-close-helper.mjs apply all --comment-and-close --execute
```

建议顺序：

1. 先跑 `verify-open` 确认当前 open issue 状态没漂移。
2. 再跑 `plan` 看本次准备处理的 issue 列表。
3. 最后才决定是否真的执行 `apply`。

## `#24` 支持全部删除

Issue: [#24](https://github.com/RoseKhlifa/Image-Studio/issues/24)

```md
这个需求当前仓库已经覆盖。

- 历史结果列表已支持“全部删除”。
- 主要落点在 `image-studio/frontend/src/components/history/HistoryRail.tsx`。
- 当前本地验证链已重新通过，2026-06-07 的 `platform-kernel` 总链状态为 `passed`。

如果你这边没有新的复现条件，这个 issue 可以关闭。
```

## `#25` 上游配置升级后的同步问题

Issue: [#25](https://github.com/RoseKhlifa/Image-Studio/issues/25)

```md
这个问题当前仓库已经做了两层处理：

- 已支持上游配置导入 / 导出。
- 兼容状态会落到宿主侧共享路径，减轻更新后重新录入配置的问题。

主要落点：

- `image-studio/frontend/src/lib/upstreamConfigTransfer.ts`
- `image-studio/backend/compatibility.go`

本地验证链已在 2026-06-07 重新通过。如果你这边没有新的“升级后配置丢失”复现路径，这个 issue 可以关闭。
```

## `#26` 失败重试开关

Issue: [#26](https://github.com/RoseKhlifa/Image-Studio/issues/26)

```md
这个需求当前仓库已经覆盖。

- 设置页已提供全局“失败自动重试”开关。
- 桌面端和 Android 端都已有入口。

主要落点：

- `image-studio/frontend/src/components/panel/SettingsPanel.tsx`
- `image-studio/frontend/src/platform/android/settings/AndroidSettingsPanel.tsx`

当前本地验证链已重新通过。如果没有新的行为差异，这个 issue 可以关闭。
```

## `#27` 上游失败重试路由功能

Issue: [#27](https://github.com/RoseKhlifa/Image-Studio/issues/27)

```md
这个需求当前仓库已经覆盖。

- 失败重试现在可以路由到用户指定的备用上游。
- 不是只做“原上游再试一次”，而是支持切到备用 profile。

主要落点：

- `go-cli/pkg/client/retry.go`
- `image-studio/frontend/src/components/panel/UpstreamProfileEditor.tsx`

本地验证链已重新通过。如果没有新的复现场景，这个 issue 可以关闭。
```

## `#28` 保存用户自定义提示词模板

Issue: [#28](https://github.com/RoseKhlifa/Image-Studio/issues/28)

```md
这个功能当前仓库已经覆盖。

已支持：

- 添加提示词模板
- 删除提示词模板
- 修改并保存模板
- 自定义模板标题
- 点击模板快速填入提示词

主要落点：

- `image-studio/frontend/src/lib/promptTemplates.ts`
- `image-studio/frontend/src/components/panel/PromptTemplateManagerModal.tsx`

本地测试与验证链已重新通过。如果没有新的补充需求，这个 issue 可以关闭。
```

## `#32` 拖结果到文件复制

Issue: [#32](https://github.com/RoseKhlifa/Image-Studio/issues/32)

```md
这个需求当前仓库已经覆盖。

- 桌面端结果图已支持直接拖出到系统文件管理器复制。
- 导出逻辑优先走真实结果文件 / 全尺寸媒体路径，不是只拖缩略图。

主要落点：

- `image-studio/backend/dialogs.go`
- `image-studio/frontend/src/lib/dragExport.ts`

本地验证链已重新通过。如果你这边没有新的拖拽失败复现，这个 issue 可以关闭。
```

## `#35` 工作区的鼠标滚动方向改为横向

Issue: [#35](https://github.com/RoseKhlifa/Image-Studio/issues/35)

```md
这个修正当前仓库已经覆盖。

- 多工作区场景下，工作区栏滚轮方向已改为横向滚动。

主要落点：

- `image-studio/frontend/src/components/layout/WorkspaceBar.tsx`

本地测试与验证链已重新通过。如果没有新的滚动行为问题，这个 issue 可以关闭。
```

## `#37` 结果里生成的多张图片查看问题

Issue: [#37](https://github.com/RoseKhlifa/Image-Studio/issues/37)

```md
这个需求当前仓库已经覆盖。

- 当前已支持在画板里对同批生成结果做左右切换浏览。
- 不需要再回到结果列表逐张找。

主要落点：

- `image-studio/frontend/src/components/canvas/useCanvasShortcuts.ts`
- `image-studio/frontend/src/state/studioStore.media.ts`

本地测试与验证链已重新通过。如果没有新的查看路径问题，这个 issue 可以关闭。
```

## `#40` 多图张数按钮选择

Issue: [#40](https://github.com/RoseKhlifa/Image-Studio/issues/40)

```md
这个需求当前仓库已经覆盖。

- Android 端多图张数选择已从滑杆改为带边界的按钮选择。

主要落点：

- `image-studio/frontend/src/platform/android/AndroidPhoneComposePanel.tsx`
- `image-studio/frontend/src/platform/android/AndroidPadComposePanel.tsx`

本地验证链已重新通过。如果没有新的交互差异，这个 issue 可以关闭。
```

## `#42` 一些功能需求

Issue: [#42](https://github.com/RoseKhlifa/Image-Studio/issues/42)

```md
这个需求集合当前仓库已覆盖主要条目。

已支持：

1. 有参考图时，图生图默认切到 `Auto` 比例。
2. 会根据参考图比例推导最接近的 2K / 4K 分辨率。
3. 支持自定义提示词模板与快捷填入。
4. 已支持结果图与参考图对比查看。

主要落点：

- `image-studio/frontend/src/components/panel/sizeCapabilities.ts`
- `image-studio/frontend/src/components/panel/PromptTemplateManagerModal.tsx`
- `image-studio/frontend/src/components/canvas/CompareOverlay.tsx`

本地验证链已重新通过。如果没有新的补充项或复现场景，这个 issue 可以关闭。
```

## 暂不建议关闭的 open issue

- `#30`：代码已修，但仍缺 Windows 真机视觉确认。
- `#36`：核心修复已落地，但仍缺 Android / Windows 真机和真实高并发上游长链路证明。
- `#23`：按当前任务约定视为上游问题，不纳入本轮收口。
- `#14`：独立在线 Web 版已明确搁置。
