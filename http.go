package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func fail(w http.ResponseWriter, err error) { http.Error(w, err.Error(), http.StatusBadRequest) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		fail(w, err)
		return false
	}
	return true
}
func (a *App) publicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		writeJSON(w, Catalog{ID: a.shelf.ID, Name: a.shelf.Name, Enabled: a.shelf.Enabled, Items: []Item{}})
	})
	mux.HandleFunc("GET /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		if a.shelf.Code != "" && r.Header.Get("X-Share-Code") != a.shelf.Code {
			http.Error(w, "需要共享码", 401)
			return
		}
		cat := Catalog{a.shelf.ID, a.shelf.Name, a.shelf.Enabled, []Item{}}
		if cat.Enabled {
			for _, item := range a.shelf.Items {
				if !item.Hidden {
					cat.Items = append(cat.Items, item)
				}
			}
		}
		writeJSON(w, cat)
	})
	mux.HandleFunc("GET /v1/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		allowed := a.shelf.Enabled && (a.shelf.Code == "" || r.Header.Get("X-Share-Code") == a.shelf.Code)
		a.mu.RUnlock()
		if !allowed {
			http.Error(w, "共享已暂停或共享码不正确", 403)
			return
		}
		a.mu.RLock()
		hidden := false
		for _, item := range a.shelf.Items {
			if item.ID == r.PathValue("id") {
				hidden = item.Hidden
				break
			}
		}
		a.mu.RUnlock()
		if hidden {
			http.NotFound(w, r)
			return
		}
		a.serveItem(w, r, r.PathValue("id"))
	})
	return mux
}
func (a *App) serveItem(w http.ResponseWriter, r *http.Request, id string) {
	a.mu.RLock()
	var item *Item
	for _, entry := range a.shelf.Items {
		if entry.ID == id {
			copy := entry
			item = &copy
			break
		}
	}
	a.mu.RUnlock()
	if item == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", item.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("download") == "1" {
		name := item.Name
		if item.Kind == "text" {
			name += ".txt"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	if item.Kind == "text" {
		http.ServeContent(w, r, item.Name, item.Created, strings.NewReader(item.Text))
		return
	}
	f, err := os.Open(filepath.Join(a.dir, "files", item.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, item.Name, item.Created, f)
}
func (a *App) localHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		peers := []Peer{}
		for _, p := range a.peers {
			p.expireItems(time.Now())
			peers = append(peers, *p)
		}
		writeJSON(w, map[string]any{"self": a.shelf, "peers": peers, "addresses": a.localAddresses(), "saveError": a.saveError})
	})
	forget := func(w http.ResponseWriter, r *http.Request) {
		count, err := a.forgetOffline(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]int{"removed": count})
	}
	mux.HandleFunc("DELETE /api/peers/{id}", forget)
	mux.HandleFunc("POST /api/peers/cleanup", forget)
	mux.HandleFunc("POST /api/text", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Text) == "" {
			fail(w, fmt.Errorf("请输入文字"))
			return
		}
		item, err := a.add("", "", body.Text, nil)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, item)
	})
	mux.HandleFunc("POST /api/files", func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			fail(w, err)
			return
		}
		added := []Item{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				fail(w, err)
				return
			}
			if part.FileName() == "" {
				part.Close()
				continue
			}
			item, err := a.add(part.FileName(), part.Header.Get("Content-Type"), "", part)
			part.Close()
			if err != nil {
				fail(w, err)
				return
			}
			added = append(added, item)
		}
		writeJSON(w, added)
	})
	mux.HandleFunc("DELETE /api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := a.remove(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Enabled  bool   `json:"enabled"`
			AutoSave *bool  `json:"autoSave"`
			Code     string `json:"code"`
		}
		if !decode(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			fail(w, fmt.Errorf("设备名称不能为空"))
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		next := a.shelf
		next.Name = strings.TrimSpace(body.Name)
		next.Enabled = body.Enabled
		if body.AutoSave != nil {
			next.AutoSave = *body.AutoSave
		}
		next.Code = body.Code
		if err := a.persist(next); err != nil {
			fail(w, err)
			return
		}
		a.shelf = next
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/items/{id}/visibility", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Hidden bool `json:"hidden"`
		}
		if !decode(w, r, &body) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		next := a.shelf
		next.Items = append([]Item{}, a.shelf.Items...)
		for i := range next.Items {
			if next.Items[i].ID == r.PathValue("id") {
				next.Items[i].Hidden = body.Hidden
				if err := a.persist(next); err != nil {
					fail(w, err)
					return
				}
				a.shelf = next
				writeJSON(w, map[string]bool{"ok": true})
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("POST /api/connect", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Address string `json:"address"`
			Code    string `json:"code"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := a.connect(body.Address, body.Code); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/unlock/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code string `json:"code"`
		}
		if !decode(w, r, &body) {
			return
		}
		a.mu.Lock()
		p := a.peers[r.PathValue("id")]
		if p == nil {
			a.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		p.Code = body.Code
		a.mu.Unlock()
		a.refresh(r.Context())
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		a.refresh(r.Context())
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/content/{peer}/{id}", a.content)
	mux.Handle("/", webHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			http.Error(w, "本机界面", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				http.Error(w, "不允许跨站访问", 403)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; media-src 'self' blob:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}
func (a *App) content(w http.ResponseWriter, r *http.Request) {
	peer, id := r.PathValue("peer"), r.PathValue("id")
	if peer == "self" {
		a.serveItem(w, r, id)
		return
	}
	a.mu.Lock()
	p := a.peers[peer]
	if p == nil {
		a.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	p.expireItems(time.Now())
	for _, item := range p.Items {
		if item.ID == id && item.Kind == "text" {
			a.mu.Unlock()
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "文字片段.txt"}))
			http.ServeContent(w, r, "文字片段.txt", item.Created, strings.NewReader(item.Text))
			return
		}
	}
	if !p.Online || !p.Enabled || p.Locked {
		a.mu.Unlock()
		http.Error(w, "设备已离线或暂停共享", http.StatusGone)
		return
	}
	addresses := append([]string{}, p.Addresses...)
	code := p.Code
	a.mu.Unlock()
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: netDialer.DialContext, ResponseHeaderTimeout: 10 * time.Second}, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for _, address := range addresses {
		req, err := http.NewRequestWithContext(r.Context(), "GET", "http://"+address+"/v1/files/"+url.PathEscape(id)+"?"+r.URL.RawQuery, nil)
		if err != nil {
			continue
		}
		req.Header.Set("X-Share-Code", code)
		req.Header.Set("Range", r.Header.Get("Range"))
		res, err := client.Do(req)
		if err != nil {
			continue
		}
		if res.StatusCode != 200 && res.StatusCode != 206 {
			res.Body.Close()
			continue
		}
		for _, key := range []string{"Content-Type", "Content-Length", "Content-Disposition", "Accept-Ranges", "Content-Range"} {
			if v := res.Header.Get(key); v != "" {
				w.Header().Set(key, v)
			}
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
		res.Body.Close()
		return
	}
	http.Error(w, "设备暂时不可达，请稍后重试", http.StatusBadGateway)
}
