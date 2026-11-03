# 邻享 Go · LinShare Go

邻享 Go 是面向 Windows 和 Android 的局域网共享工具。把文件、图片、视频或文字放进共享空间，同一网络中的另一台设备即可按需查看和保存，无需云端账号。

当前版本：**1.1.1**。

## 下载与使用

在 [Releases](https://github.com/yun-666-666/linshare-go/releases/latest) 下载：

- Windows x64：`LinShare-Go-1.1.1-windows-x64.zip`，解压后运行 `邻享-Go.exe`。需要 [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)。
- Android 10 及以上、ARM64：`LinShare-Go-1.1.1-android-arm64.apk`。当前安装包沿用本地开发签名及调试构建设置。

两台设备连接同一局域网后，启动邻享并添加共享内容。设备通常会自动发现，也可以在“设备与连接”里输入对方地址。可设置共享码、暂停共享，并选择自动保存附近设备的文件。

当前传输使用局域网 HTTP，共享码不提供传输加密；请在可信网络中使用。Android 后台共享可能需要允许通知及后台运行。

## 功能

- 共享文件、图片、视频和文字，按需获取内容。
- 自动发现附近设备，也支持手动连接。
- 手机端双列内容卡片，Windows 独立桌面窗口。
- 可隐藏或移除共享条目、设置共享码、暂停共享。
- 搜索文字、文件名与设备名，按文字、图片、视频、文件筛选。
- 内容管理实时显示在线状态和数量，可清理离线设备记录。
- 离线文件缓存 30 秒、文字缓存 10 分钟，过期自动清理；本机共享与已下载文件保留。
- 未变化的内容卡片在刷新时保留，减少图片重载与视频播放中断。
- 自动保存到下载目录下的“邻享”分类文件夹。

## 从源码构建

需要 Go 1.24 或以上、Windows PowerShell；构建 Android 还需要 JDK 21、Android SDK Platform 36 和 Build Tools 36.0.0。

```powershell
go test ./...
./build.ps1 -Go 'C:/path/to/go/bin/go.exe' -Sdk 'C:/path/to/android-sdk' -Jdk 'C:/path/to/jdk-21'
```

构建结果位于 `out/邻享-Go.exe` 和 `out/邻享-Go.apk`。脚本会生成 Windows 图标资源，并在用户目录 `.android/linshare-go.jks` 使用或创建本地开发签名。此密钥不包含在仓库中，自行构建的签名可能与发布包不同。

源码分为 Go 共享服务、`web/` 界面、`android/` 原生壳和 `windows/` 桌面资源。局域网服务默认使用 TCP/UDP 48771 和组播 UDP 48772；本机界面监听 `127.0.0.1:48773`。

## 协议

按作者要求，本项目沿用轻记的 [个人非商业学习许可 1.0](LICENSE)，保留原协议名称与完整条款。允许个人非商业学习、研究和参考，商业使用及其他未授权用途需要另行取得许可。

**这是源码公开、限制商业使用的许可，不属于 OSI 定义的开源许可证。** 第三方材料仍遵循各自原有许可证，不受本项目许可替代。

## 参考与致谢

感谢以下项目的作者、贡献者和维护者：

- [LocalSend](https://github.com/localsend/localsend)：本项目早期 Flutter 版本基于 LocalSend 1.18.2，感谢其局域网文件共享方案及实现带来的参考。当前仓库发布 Go 重写版本，使用独立的 `linshare-go/1` 协议，不是 LocalSend 官方版本，也不与 LocalSend 客户端互通。LocalSend 采用 Apache-2.0，版权归 Tien Do Nam 及贡献者所有。
- [go-webview2](https://github.com/jchv/go-webview2)：Windows WebView2 窗口集成，MIT 许可；感谢 John Chadwick、Serge Zaitsev 等贡献者。
- [go-winloader](https://github.com/jchv/go-winloader)：Windows 加载支持，ISC 许可；感谢 John Chadwick。
- [Go 与 golang.org/x/sys](https://go.dev/)：共享服务运行时与 Windows 系统接口，BSD 许可；感谢 Go Authors。
- [rsrc](https://github.com/akavel/rsrc)：Windows 图标和清单资源构建工具，MIT 许可；感谢 rsrc Authors。
- [Microsoft WebView2](https://developer.microsoft.com/microsoft-edge/webview2/)：Windows 网页运行环境；运行时遵循微软提供的许可条款。

第三方声明和许可全文见 [NOTICE](NOTICE)、[THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt) 和 [LocalSend Apache-2.0 许可](licenses/LocalSend-Apache-2.0.txt)。
