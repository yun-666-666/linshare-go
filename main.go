package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed web/*
var assets embed.FS

func main() {
	base, _ := os.UserConfigDir()
	dir := flag.String("data", filepath.Join(base, "LinShareGo"), "shared content directory")
	port := flag.Int("port", 48771, "LAN port")
	ui := flag.Int("ui", 48773, "local interface port")
	name := flag.String("name", "", "device name")
	noOpen := flag.Bool("no-open", false, "do not open interface")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0700); err != nil {
		log.Fatal(err)
	}
	logFile, err := os.OpenFile(filepath.Join(*dir, "service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	uiURL := fmt.Sprintf("http://127.0.0.1:%d", *ui)
	if !*noOpen && runningInstance(uiURL) {
		openWindow(uiURL)
		return
	}
	app, err := openApp(*dir, *name)
	if err != nil {
		log.Fatal(err)
	}
	lan, err := net.Listen("tcp4", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	local, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *ui))
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	app.port = lan.Addr().(*net.TCPAddr).Port
	publicServer := &http.Server{Handler: app.publicHandler(), ReadHeaderTimeout: 10 * time.Second}
	localServer := &http.Server{Handler: app.localHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := publicServer.Serve(lan); err != http.ErrServerClosed {
			log.Print(err)
			stop()
		}
	}()
	go func() {
		if err := localServer.Serve(local); err != http.ErrServerClosed {
			log.Print(err)
			stop()
		}
	}()
	app.uiURL = "http://" + local.Addr().String()
	if runtime.GOOS != "android" {
		go app.autoSaveLoop(ctx)
	}
	go app.discover(ctx)
	go app.refreshLoop(ctx)
	url := "http://" + local.Addr().String()
	_ = os.WriteFile(filepath.Join(*dir, "ready.json"), []byte(fmt.Sprintf(`{"url":%q,"pid":%d}`, url, os.Getpid())), 0600)
	log.Printf("ready %s LAN %d", url, app.port)
	if !*noOpen {
		openWindow(url)
	}
	<-ctx.Done()
	_ = localServer.Close()
	_ = publicServer.Close()
}

func webHandler() http.Handler { sub, _ := fs.Sub(assets, "web"); return http.FileServer(http.FS(sub)) }
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// Reopening the launcher reuses the live service after its window is closed.
func runningInstance(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(url + "/api/state")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var state struct {
		Self Shelf `json:"self"`
	}
	return res.StatusCode == http.StatusOK && json.NewDecoder(res.Body).Decode(&state) == nil && state.Self.ID != ""
}
