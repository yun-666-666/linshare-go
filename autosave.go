package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func saveCategory(kind string) string {
	switch kind {
	case "image":
		return "图片"
	case "video":
		return "视频"
	default:
		return "文件"
	}
}

func (a *App) autoSaveLoop(ctx context.Context) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	root := filepath.Join(home, "Downloads", "邻享")
	saved := map[string]string{}
	ledger := filepath.Join(a.dir, "saved.json")
	if data, err := os.ReadFile(ledger); err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	if saved == nil {
		saved = map[string]string{}
	}
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		a.autoSaveOnce(ctx, root, saved, ledger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) autoSaveOnce(ctx context.Context, root string, saved map[string]string, ledger string) {
	a.mu.RLock()
	enabled := a.shelf.AutoSave
	peers := []Peer{}
	for _, p := range a.peers {
		peers = append(peers, *p)
	}
	a.mu.RUnlock()
	if !enabled {
		return
	}
	var saveErr error
	for _, p := range peers {
		if !p.Online || !p.Enabled || p.Locked {
			continue
		}
		for _, item := range p.Items {
			a.mu.RLock()
			enabled = a.shelf.AutoSave
			a.mu.RUnlock()
			if !enabled || ctx.Err() != nil {
				return
			}
			key := p.ID + "/" + item.ID
			if item.Kind == "text" || saved[key] != "" {
				continue
			}
			path, err := a.saveRemote(ctx, root, p.ID, item)
			if err != nil {
				saveErr = err
				continue
			}
			saved[key] = path
			if err := atomicJSON(ledger, saved); err != nil {
				saveErr = err
			}
		}
	}
	a.mu.Lock()
	a.saveError = ""
	if saveErr != nil {
		a.saveError = "自动保存失败，将重试：" + saveErr.Error()
	}
	a.mu.Unlock()
}

func (a *App) saveRemote(ctx context.Context, root, peer string, item Item) (string, error) {
	name := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, item.Name)
	dir := filepath.Join(root, saveCategory(item.Kind))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".接收-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, "GET", a.uiURL+"/api/content/"+url.PathEscape(peer)+"/"+url.PathEscape(item.ID), nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("%s：HTTP %d", item.Name, res.StatusCode)
	}
	n, err := io.Copy(f, res.Body)
	if err != nil {
		return "", err
	}
	if n != item.Size {
		return "", fmt.Errorf("%s 未传完", item.Name)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// Reserve a unique destination without overwriting a user's existing file.
	path := filepath.Join(dir, name)
	for i := 1; ; i++ {
		reserved, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if os.IsExist(err) {
			path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, filepath.Ext(name)), i, filepath.Ext(name)))
			continue
		}
		if err != nil {
			return "", err
		}
		reserved.Close()
		break
	}
	if err := os.Rename(f.Name(), path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
