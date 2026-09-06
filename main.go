package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	stdRuntime "runtime"
	"sync"

	"github.com/mxschmitt/playwright-go"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "北师大选课助手",
		Width:     1024,
		Height:    768,
		MinWidth:  640,
		MinHeight: 512,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 0},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Frameless: true,
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         2,
		},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
		},
		Debug: options.Debug{
			OpenInspectorOnStartup: false,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "2756010b-ad2d-48c3-be36-d586ce7b8af0",
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// App struct
type App struct {
	ctx          context.Context
	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	installMu    sync.Mutex
	browserReady bool
	browserPath  string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// 第二个进程的开启回调
func (a *App) onSecondInstanceLaunch(secondInstanceData options.SecondInstanceData) {
	runtime.WindowUnminimise(a.ctx)
	runtime.Show(a.ctx)
}

// 安装浏览器
func (a *App) InstallBrowser() error {
	a.installMu.Lock()
	defer a.installMu.Unlock()
	if a.browserReady {
		return nil
	}
	// Finder launches do not inherit the terminal PATH. Reuse an installed Node
	// runtime when present, including on macOS, before downloading a separate one.
	if os.Getenv("PLAYWRIGHT_NODEJS_PATH") == "" {
		nodePath, _ := exec.LookPath("node")
		if nodePath == "" && stdRuntime.GOOS == "darwin" {
			nodePath = firstRegularFile([]string{"/opt/homebrew/bin/node", "/usr/local/bin/node"})
		}
		if nodePath != "" {
			if err := os.Setenv("PLAYWRIGHT_NODEJS_PATH", nodePath); err != nil {
				return fmt.Errorf("无法配置浏览器运行组件")
			}
		}
	}
	// Install the small driver first. Playwright officially supports installed
	// Chrome and Edge channels, which avoids a large browser download on machines
	// that already have one.
	if err := playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true}); err != nil {
		return fmt.Errorf("浏览器驱动安装失败，请检查网络后重试")
	}
	if path := installedChromiumPath(); path != "" {
		a.browserPath = path
		a.browserReady = true
		return nil
	}
	if err := playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}}); err != nil {
		return fmt.Errorf("Chromium 安装失败，请检查网络后重试")
	}
	a.browserReady = true
	return nil
}

func installedChromiumPath() string {
	var candidates []string
	switch stdRuntime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			filepath.Join(home, "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
		}
	case "windows":
		for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, "Google/Chrome/Application/chrome.exe"),
				filepath.Join(root, "Microsoft/Edge/Application/msedge.exe"))
		}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable"} {
			if path, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, path)
			}
		}
	}
	return firstRegularFile(candidates)
}

func firstRegularFile(candidates []string) string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func (a *App) shutdown(_ context.Context) { _ = a.StopCourses() }

// 对话框
func (a *App) Dialog(dialogType, message string) (string, error) {
	switch dialogType {
	case "info":
		return runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
			Type:    runtime.InfoDialog,
			Title:   "提示",
			Message: message,
		})
	case "warning":
		return runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
			Type:    runtime.WarningDialog,
			Title:   "警告",
			Message: message,
		})
	case "error":
		return runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "错误",
			Message: message,
		})
	case "question":
		return runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
			Type:          runtime.QuestionDialog,
			Title:         "提示",
			Message:       message,
			Buttons:       []string{"Yes", "No"},
			DefaultButton: "Yes",
			CancelButton:  "No",
		})
	default:
		return "", fmt.Errorf("未知的对话框类型")
	}
}
