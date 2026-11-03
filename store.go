package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Item struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	MIME    string    `json:"mime"`
	Size    int64     `json:"size"`
	Text    string    `json:"text,omitempty"`
	Created time.Time `json:"created"`
	Hidden  bool      `json:"hidden,omitempty"`
}
type Shelf struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	AutoSave bool   `json:"autoSave"`
	Code     string `json:"code,omitempty"`
	Items    []Item `json:"items"`
}
type Peer struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Addresses    []string  `json:"addresses"`
	Items        []Item    `json:"items"`
	Online       bool      `json:"online"`
	Enabled      bool      `json:"enabled"`
	Locked       bool      `json:"locked"`
	Updated      time.Time `json:"updated"`
	OfflineSince time.Time `json:"offlineSince"`
	Error        string    `json:"error,omitempty"`
	Code         string    `json:"-"`
}
type App struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	dir       string
	shelf     Shelf
	peers     map[string]*Peer
	port      int
	uiURL     string
	saveError string
	client    *http.Client
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func openApp(dir, name string) (*App, error) {
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0700); err != nil {
		return nil, err
	}
	a := &App{dir: dir, peers: map[string]*Peer{}, client: &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: netDialer.DialContext}, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}
	data, err := os.ReadFile(filepath.Join(dir, "shelf.json"))
	if err == nil {
		if err = json.Unmarshal(data, &a.shelf); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		if name == "" {
			name, _ = os.Hostname()
		}
		a.shelf = Shelf{ID: randomID(), Name: name, Enabled: true, Items: []Item{}}
		if err = a.persist(a.shelf); err != nil {
			return nil, err
		}
	}
	if a.shelf.Items == nil {
		a.shelf.Items = []Item{}
	}
	if data, err = os.ReadFile(filepath.Join(dir, "peers.json")); err == nil {
		var peers []Peer
		if json.Unmarshal(data, &peers) == nil {
			for i := range peers {
				p := peers[i]
				if p.Online || p.OfflineSince.IsZero() {
					p.OfflineSince = p.Updated
					if p.OfflineSince.IsZero() {
						p.OfflineSince = time.Now()
					}
				}
				p.Online = false
				p.Error = "等待设备重新连接"
				p.expireItems(time.Now())
				a.peers[p.ID] = &p
			}
		}
	}
	return a, nil
}

// Remote metadata expires; local shared files and saved downloads are untouched.
func (p *Peer) expireItems(now time.Time) {
	if p.Online || p.OfflineSince.IsZero() {
		return
	}
	kept := make([]Item, 0, len(p.Items))
	for _, item := range p.Items {
		ttl := 30 * time.Second
		if item.Kind == "text" {
			ttl = 10 * time.Minute
		}
		if now.Sub(p.OfflineSince) < ttl {
			kept = append(kept, item)
		}
	}
	p.Items = kept
}

func (a *App) forgetOffline(id string) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := []Peer{}
	removed := []string{}
	for key, p := range a.peers {
		if !p.Online && (id == "" || key == id) {
			removed = append(removed, key)
		} else {
			kept = append(kept, *p)
		}
	}
	if id != "" && len(removed) == 0 {
		return 0, errors.New("设备已上线或记录已清理，请刷新后重试")
	}
	if err := atomicJSON(filepath.Join(a.dir, "peers.json"), kept); err != nil {
		return 0, err
	}
	for _, key := range removed {
		delete(a.peers, key)
	}
	return len(removed), nil
}
func atomicJSON(path string, v any) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".save-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = json.NewEncoder(f).Encode(v); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
func (a *App) persist(s Shelf) error { return atomicJSON(filepath.Join(a.dir, "shelf.json"), s) }
func (a *App) add(name, typ, text string, r io.Reader) (Item, error) {
	item := Item{ID: randomID(), Name: filepath.Base(strings.ReplaceAll(name, "\\", "/")), MIME: typ, Text: text, Created: time.Now(), Kind: "file"}
	if text != "" {
		item.Kind = "text"
		item.MIME = "text/plain; charset=utf-8"
		item.Size = int64(len(text))
		item.Name = "文字片段"
	} else {
		if item.MIME == "" || item.MIME == "application/octet-stream" {
			item.MIME = mime.TypeByExtension(filepath.Ext(item.Name))
		}
		if strings.HasPrefix(item.MIME, "image/") {
			item.Kind = "image"
		}
		if strings.HasPrefix(item.MIME, "video/") {
			item.Kind = "video"
		}
		path := filepath.Join(a.dir, "files", item.ID)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return item, err
		}
		item.Size, err = io.Copy(f, r)
		if err == nil {
			err = f.Sync()
		}
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
			return item, err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.shelf
	next.Items = append(append([]Item{}, a.shelf.Items...), item)
	if err := a.persist(next); err != nil {
		if item.Kind != "text" {
			_ = os.Remove(filepath.Join(a.dir, "files", item.ID))
		}
		return item, err
	}
	a.shelf = next
	return item, nil
}
func (a *App) remove(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.shelf
	next.Items = []Item{}
	var found *Item
	for _, item := range a.shelf.Items {
		if item.ID == id {
			copy := item
			found = &copy
		} else {
			next.Items = append(next.Items, item)
		}
	}
	if found == nil {
		return errors.New("内容不存在")
	}
	if err := a.persist(next); err != nil {
		return err
	}
	a.shelf = next
	if found.Kind != "text" {
		_ = os.Remove(filepath.Join(a.dir, "files", id))
	}
	return nil
}
