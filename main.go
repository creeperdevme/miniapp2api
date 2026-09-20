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
	addr := flag.String("addr", "", "listen address, e.g. 127.0.0.1:8787 (default: read from config.json)")
	dataDir := flag.String("data", "", "data directory holding config.json and auths/ (default: the executable's folder)")
	noBrowser := flag.Bool("no-browser", false, "do not open a browser after startup")
	flag.Parse()

	logger := log.New(os.Stdout, "[miniapp2api] ", log.LstdFlags)

	root, err := resolveDataDir(*dataDir)
	if err != nil {
		logger.Fatalf("cannot resolve the data directory: %v", err)
	}

	cfg, err := config.Load(filepath.Join(root, config.FileName))
	if err != nil {
		logger.Fatalf("%v", err)
	}
	if *addr != "" {
		if err := cfg.SetListenAddr(*addr); err != nil {
			logger.Fatalf("invalid -addr value: %v", err)
		}
	}
	generated, err := cfg.EnsureAPIKey()
	if err != nil {
		logger.Fatalf("%v", err)
	}
	if generated {
		if err := cfg.Save(); err != nil {
			logger.Fatalf("cannot write %s: %v", cfg.Path(), err)
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

	printBanner(cfg, pool, root)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", cfg.ListenAddr())
	if err != nil {
		if isAddrInUse(err) {
			logger.Fatalf("port %s is already in use; start again with -addr 127.0.0.1:<another port>", cfg.ListenAddr())
		}
		logger.Fatalf("cannot start the server: %v", err)
	}

	if !*noBrowser {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := openBrowser(browserURL(cfg)); err != nil {
				logger.Printf("could not open a browser, please visit %s manually", browserURL(cfg))
			}
		}()
	}

	go func() {
		<-ctx.Done()
		logger.Printf("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server stopped: %v", err)
	}
	logger.Printf("stopped")
}

func printBanner(cfg *config.Config, pool *store.Store, root string) {
	summary := pool.Summary()
	line := strings.Repeat("─", 62)

	fmt.Println(line)
	fmt.Printf(" miniapp2api v%s\n", server.Version)
	fmt.Println(line)
	fmt.Printf(" Data dir    %s\n", root)
	fmt.Printf(" Web UI      %s\n", browserURL(cfg))
	fmt.Printf(" OpenAI API  %s\n", cfg.BaseURL())
	fmt.Printf(" Accounts    %d total (%d enabled / %d disabled)\n", summary["total"], summary["enabled"], summary["disabled"])
	fmt.Println(line)

	if !cfg.HasPassword() {
		fmt.Println(" First run: open the Web UI and set an admin password.")
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
