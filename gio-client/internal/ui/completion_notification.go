package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	sharedCompat "image-studio/shared/compat"
)

type systemNotificationPermissionState string

const (
	systemNotificationPermissionDefault     systemNotificationPermissionState = "default"
	systemNotificationPermissionGranted     systemNotificationPermissionState = "granted"
	systemNotificationPermissionDenied      systemNotificationPermissionState = "denied"
	systemNotificationPermissionUnsupported systemNotificationPermissionState = "unsupported"
)

var readSystemNotificationPermissionFunc = readSystemNotificationPermission
var requestSystemNotificationPermissionFunc = requestSystemNotificationPermission
var showSystemNotificationFunc = showSystemNotification

func systemNotificationPermissionLabel(permission systemNotificationPermissionState) string {
	switch permission {
	case systemNotificationPermissionGranted:
		return "已允许"
	case systemNotificationPermissionDenied:
		return "已拒绝"
	case systemNotificationPermissionUnsupported:
		return "当前平台不支持"
	default:
		return "尚未授权"
	}
}

func completionNotificationPermissionActionLabel(permission systemNotificationPermissionState) string {
	switch permission {
	case systemNotificationPermissionGranted:
		return "重新检查权限"
	case systemNotificationPermissionUnsupported:
		return "检查支持"
	default:
		return "申请权限"
	}
}

func readSystemNotificationPermission() systemNotificationPermissionState {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("osascript"); err == nil {
			return systemNotificationPermissionGranted
		}
	case "windows":
		if _, err := exec.LookPath("powershell"); err == nil {
			return systemNotificationPermissionGranted
		}
	default:
		if _, err := exec.LookPath("notify-send"); err == nil {
			return systemNotificationPermissionGranted
		}
		if _, err := exec.LookPath("zenity"); err == nil {
			return systemNotificationPermissionGranted
		}
		if _, err := exec.LookPath("kdialog"); err == nil {
			return systemNotificationPermissionGranted
		}
	}
	return systemNotificationPermissionUnsupported
}

func requestSystemNotificationPermission() systemNotificationPermissionState {
	return readSystemNotificationPermission()
}

func shouldSendCompletionNotification(config sharedCompat.CompletionNotificationSettings, completedNow int, totalNow int, windowFocused bool) bool {
	next := normaliseCompletionNotificationSettings(&config)
	return next.Enabled && !windowFocused && totalNow > 0 && completedNow == totalNow
}

func completionNotificationBody(item sharedCompat.HistoryItem) string {
	if text := strings.TrimSpace(item.RevisedPrompt); text != "" {
		return text
	}
	if text := strings.TrimSpace(item.Prompt); text != "" {
		return text
	}
	if strings.TrimSpace(item.Mode) == "edit" {
		return "图片编辑任务已完成"
	}
	return "图片生成任务已完成"
}

func (a *App) setCompletionNotificationEnabled(value bool) systemNotificationPermissionState {
	if !value {
		a.completionNotification.Enabled = false
		a.completionNotificationPermission = readSystemNotificationPermissionFunc()
		a.appendLog("已关闭系统通知")
		return a.completionNotificationPermission
	}
	permission := requestSystemNotificationPermissionFunc()
	a.completionNotificationPermission = permission
	if permission == systemNotificationPermissionGranted {
		a.completionNotification.Enabled = true
		a.appendLog("已开启系统通知")
		return permission
	}
	switch permission {
	case systemNotificationPermissionUnsupported:
		a.appendLog("当前平台不支持系统通知")
	case systemNotificationPermissionDenied:
		a.appendLog("系统通知权限已被拒绝，请在系统设置中允许后重试")
	default:
		a.appendLog("请先允许系统通知权限")
	}
	return permission
}

func (a *App) refreshCompletionNotificationPermission() systemNotificationPermissionState {
	permission := requestSystemNotificationPermissionFunc()
	a.completionNotificationPermission = permission
	switch permission {
	case systemNotificationPermissionGranted:
		a.appendLog("系统通知权限已授权")
	case systemNotificationPermissionDenied:
		a.appendLog("系统通知权限已被拒绝，请在系统设置中允许后重试")
	case systemNotificationPermissionUnsupported:
		a.appendLog("当前平台不支持系统通知")
	default:
		a.appendLog("系统通知尚未授权")
	}
	return permission
}

func (a *App) maybeSendCompletionNotification(item sharedCompat.HistoryItem, completedNow int, totalNow int) {
	a.mu.Lock()
	config := a.completionNotification
	permission := a.completionNotificationPermission
	windowFocused := a.windowFocused
	a.mu.Unlock()
	if permission != systemNotificationPermissionGranted {
		return
	}
	if !shouldSendCompletionNotification(config, completedNow, totalNow, windowFocused) {
		return
	}
	title := "Image Studio · 已完成"
	body := completionNotificationBody(item)
	go func() {
		if err := showSystemNotificationFunc(title, body); err != nil {
			a.appendLog("发送系统通知失败: " + err.Error())
		}
	}()
}

func showSystemNotification(title string, body string) error {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		title = "Image Studio"
	}
	if body == "" {
		body = "图片任务已完成"
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s", appleScriptQuoted(body), appleScriptQuoted(title))
		return exec.Command("osascript", "-e", script).Run()
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $n = New-Object System.Windows.Forms.NotifyIcon; $n.Icon = [System.Drawing.SystemIcons]::Information; $n.BalloonTipTitle = $args[0]; $n.BalloonTipText = $args[1]; $n.Visible = $true; $n.ShowBalloonTip(5000); Start-Sleep -Milliseconds 5500; $n.Dispose()`
		return exec.Command("powershell", "-NoProfile", "-Command", script, title, body).Run()
	default:
		if path, err := exec.LookPath("notify-send"); err == nil {
			return exec.Command(path, title, body).Run()
		}
		if path, err := exec.LookPath("zenity"); err == nil {
			return exec.Command(path, "--notification", "--text="+title+"\n"+body).Run()
		}
		if path, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command(path, "--title", title, "--passivepopup", body, "5").Run()
		}
		return fmt.Errorf("当前平台没有可用的系统通知器")
	}
}

func appleScriptQuoted(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}
