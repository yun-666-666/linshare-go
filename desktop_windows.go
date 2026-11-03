//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

func openWindow(url string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32 := windows.NewLazySystemDLL("user32.dll")
	title := windows.StringToUTF16Ptr("邻享")
	class := windows.StringToUTF16Ptr("webview")
	existing, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if existing != 0 {
		user32.NewProc("ShowWindow").Call(existing, 9)
		user32.NewProc("SetForegroundWindow").Call(existing)
		return
	}
	if _, err := webviewloader.GetInstalledVersion(); err != nil {
		desktopError("需要 Microsoft WebView2 Runtime 才能打开邻享。请从微软官网安装后重试。")
		return
	}
	user32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3))
	data, _ := os.UserConfigDir()
	w := webview.NewWithOptions(webview.WebViewOptions{
		AutoFocus:     true,
		DataPath:      filepath.Join(data, "LinShareGo", "desktop-webview"),
		WindowOptions: webview.WindowOptions{Title: "邻享", Width: 1180, Height: 840, Center: true, IconId: 2},
	})
	if w == nil {
		desktopError("邻享窗口未能启动，请重新打开应用。详情可查看共享服务日志。")
		return
	}
	defer w.Destroy()
	w.SetSize(640, 540, webview.HintMin)
	dwmSetAttribute := windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	// DWM colors use COLORREF (0x00BBGGRR), matching the dark sharing surface.
	for _, attribute := range []struct{ id, value uint32 }{
		{20, 1},          // Dark native window controls.
		{33, 2},          // Rounded window corners.
		{34, 0x005b463c}, // Border: #3c465b.
		{35, 0x0033231b}, // Caption: #1b2333.
		{36, 0x00f5e5dc}, // Title: #dce5f5.
	} {
		dwmSetAttribute.Call(uintptr(w.Window()), uintptr(attribute.id), uintptr(unsafe.Pointer(&attribute.value)), 4)
	}
	w.Init("document.addEventListener('DOMContentLoaded',()=>document.documentElement.classList.add('desktop'))")
	w.Navigate(url)
	w.Run()
}

func desktopError(message string) {
	log.Print(message)
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("邻享"))), 0x10)
}
