// miniapp2api 把 miniapps.ai 的帳號池包成 OpenAI 相容的 /v1 API，
// 並提供一個網頁介面來管理號池。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"miniapp2api/internal/config"
	"miniapp2api/internal/server"
	"miniapp2api/internal/store"
)

func main() {
	addr := flag.String("addr", "", "監聽位址，例如 127.0.0.1:8787（預設讀取 config.json）")
	dataDir := flag.String("data", "", "資料目錄，存放 config.json 與 auths/（預設為執行檔所在目錄）")
	noBrowser := flag.Bool("no-browser", false, "啟動後不要自動開啟瀏覽器")
	flag.Parse()

	logger := log.New(os.Stdout, "[miniapp2api] ", log.LstdFlags)

	root, err := resolveDataDir(*dataDir)
	if err != nil {
		logger.Fatalf("無法決定資料目錄：%v", err)
	}

	cfg, err := config.Load(filepath.Join(root, config.FileName))
	if err != nil {
		logger.Fatalf("%v", err)
	}
	if *addr != "" {
		if err := cfg.SetListenAddr(*addr); err != nil {
			logger.Fatalf("位址參數錯誤：%v", err)
		}
	}
	generated, err := cfg.EnsureAPIKey()
	if err != nil {
		logger.Fatalf("%v", err)
	}
	if generated {
		if err := cfg.Save(); err != nil {
			logger.Fatalf("無法寫入 %s：%v", cfg.Path(), err)
		}
	}

	pool, err := store.Open(root)
	if err != nil {
		logger.Fatalf("%v", err)
	}

	handler := server.New(cfg, pool, logger)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           handler.Handler(),
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	printBanner(cfg, pool, root, generated)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", cfg.ListenAddr())
	if err != nil {
		if isAddrInUse(err) {
			logger.Fatalf("連接埠 %s 已被占用，請用 -addr 127.0.0.1:其他連接埠 啟動。", cfg.ListenAddr())
		}
		logger.Fatalf("無法啟動伺服器：%v", err)
	}

	if !*noBrowser {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := openBrowser(browserURL(cfg)); err != nil {
				logger.Printf("無法自動開啟瀏覽器，請手動前往 %s", browserURL(cfg))
			}
		}()
	}

	go func() {
		<-ctx.Done()
		logger.Printf("正在關閉伺服器…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("伺服器結束：%v", err)
	}
	logger.Printf("已停止。")
}

func printBanner(cfg *config.Config, pool *store.Store, root string, generatedKey bool) {
	summary := pool.Summary()
	line := strings.Repeat("─", 62)

	fmt.Println(line)
	fmt.Printf(" miniapp2api v%s\n", server.Version)
	fmt.Println(line)
	fmt.Printf(" 資料目錄   %s\n", root)
	fmt.Printf(" 網頁介面   %s\n", browserURL(cfg))
	fmt.Printf(" OpenAI API %s\n", cfg.BaseURL())
	if generatedKey {
		// 剛產生的金鑰只顯示這一次，之後只能從網頁介面重新產生。
		fmt.Printf(" API 金鑰   %s\n", cfg.Key())
		fmt.Println("            這是剛產生的金鑰，只會顯示這一次，請立刻保存；")
		fmt.Println("            之後要查看請在網頁介面按「重新產生 API 金鑰」。")
	} else {
		fmt.Printf(" API 金鑰   %s（完整金鑰請在網頁介面重新產生）\n", config.MaskKey(cfg.Key()))
	}
	if cfg.RequireKey() {
		fmt.Printf("            呼叫時請帶 Authorization: Bearer <API 金鑰>\n")
	}
	fmt.Printf(" 號池       %d 個帳號（啟用 %d／停用 %d）\n", summary["total"], summary["enabled"], summary["disabled"])
	fmt.Println(line)

	if !cfg.HasPassword() {
		fmt.Println(" ⚠ 首次啟動：請在網頁上設定一組登入密碼。")
		fmt.Println(line)
	}
}

func resolveDataDir(flagValue string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		return filepath.Abs(flagValue)
	}
	executable, err := os.Executable()
	if err == nil {
		return filepath.Dir(executable), nil
	}
	return os.Getwd()
}

func browserURL(cfg *config.Config) string {
	addr := cfg.ListenAddr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/", host, port)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return strings.Contains(strings.ToLower(opErr.Error()), "only one usage") ||
			strings.Contains(strings.ToLower(opErr.Error()), "address already in use")
	}
	return false
}
