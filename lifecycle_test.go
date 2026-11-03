package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func localCall(a *App, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	a.localHandler().ServeHTTP(w, httptest.NewRequest(method, "http://127.0.0.1:48773"+path, strings.NewReader(body)))
	return w
}
func TestOfflineClockAndCachedText(t *testing.T) {
	sender, receiver := newTestApp(t), newTestApp(t)
	item, _ := sender.add("", "", "离线仍可保存的文字", nil)
	file, _ := sender.add("a.txt", "text/plain", "", strings.NewReader("file"))
	server := httptest.NewServer(sender.publicHandler())
	if err := receiver.connect(strings.TrimPrefix(server.URL, "http://"), ""); err != nil {
		t.Fatal(err)
	}
	peer := receiver.peers[sender.shelf.ID]
	server.Close()
	receiver.refresh(context.Background())
	since := peer.OfflineSince
	if since.IsZero() {
		t.Fatal("missing offline start")
	}
	receiver.refresh(context.Background())
	if !peer.OfflineSince.Equal(since) {
		t.Fatal("retry reset expiration")
	}
	w := localCall(receiver, "GET", "/api/content/"+peer.ID+"/"+item.ID, "")
	if w.Code != 200 || w.Body.String() != item.Text {
		t.Fatalf("cached text: %d %s", w.Code, w.Body.String())
	}
	w = localCall(receiver, "GET", "/api/content/"+peer.ID+"/"+file.ID, "")
	if w.Code != http.StatusGone {
		t.Fatal("offline file remained available")
	}
	server = httptest.NewServer(sender.publicHandler())
	defer server.Close()
	peer.Addresses = []string{strings.TrimPrefix(server.URL, "http://")}
	receiver.refresh(context.Background())
	if !peer.Online || !peer.OfflineSince.IsZero() {
		t.Fatal("reconnect failed")
	}
}
func TestVisibilityAndSettingsPersist(t *testing.T) {
	a := newTestApp(t)
	item, _ := a.add("", "", "保留但不共享", nil)
	for _, hidden := range []bool{true, false} {
		body, _ := json.Marshal(map[string]bool{"hidden": hidden})
		w := localCall(a, "POST", "/api/items/"+item.ID+"/visibility", string(body))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		w = httptest.NewRecorder()
		a.publicHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://peer/v1/catalog", nil))
		if strings.Contains(w.Body.String(), item.Text) == hidden {
			t.Fatal("catalog visibility wrong")
		}
		w = httptest.NewRecorder()
		a.publicHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://peer/v1/files/"+item.ID, nil))
		if (w.Code == 200) == hidden {
			t.Fatal("hidden content served")
		}
	}
	w := localCall(a, "POST", "/api/settings", `{"name":"测试","enabled":false,"autoSave":true}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reopened, err := openApp(a.dir, "")
	if err != nil || reopened.shelf.Enabled || !reopened.shelf.AutoSave || len(reopened.shelf.Items) != 1 {
		t.Fatal("settings lost content or preferences")
	}
	w = localCall(a, "POST", "/api/settings", `{"name":"测试","enabled":true}`)
	if w.Code != 200 || !a.shelf.AutoSave {
		t.Fatal("sharing toggle reset autosave")
	}
}
func TestAutoSaveAndReopen(t *testing.T) {
	sender, receiver := newTestApp(t), newTestApp(t)
	file, _ := sender.add("a.txt", "text/plain", "", strings.NewReader("complete contents"))
	_, _ = sender.add("", "", "do not autosave text", nil)
	remote := httptest.NewServer(sender.publicHandler())
	defer remote.Close()
	if err := receiver.connect(strings.TrimPrefix(remote.URL, "http://"), ""); err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(receiver.localHandler())
	defer local.Close()
	receiver.uiURL = local.URL
	if !runningInstance(local.URL) {
		t.Fatal("existing service not recognized")
	}
	root := t.TempDir()
	saved := map[string]string{}
	ledger := filepath.Join(receiver.dir, "saved.json")
	receiver.autoSaveOnce(context.Background(), root, saved, ledger)
	if len(saved) != 0 {
		t.Fatal("disabled autosave downloaded")
	}
	receiver.shelf.AutoSave = true
	receiver.autoSaveOnce(context.Background(), root, saved, ledger)
	if len(saved) != 1 {
		t.Fatalf("autosave failed: %s", receiver.saveError)
	}
	data, err := os.ReadFile(saved[sender.shelf.ID+"/"+file.ID])
	if err != nil || string(data) != "complete contents" {
		t.Fatal("wrong saved content")
	}
	receiver.autoSaveOnce(context.Background(), root, saved, ledger)
	files, _ := os.ReadDir(filepath.Join(root, "文件"))
	if len(files) != 1 {
		t.Fatal("duplicate auto download")
	}
	// A truncated response must not become a completed file or success record.
	sender.shelf.Items[0].Size += 10
	sender.shelf.Items[0].ID = "broken"
	_ = os.WriteFile(filepath.Join(sender.dir, "files", "broken"), []byte("short"), 0600)
	receiver.refresh(context.Background())
	receiver.autoSaveOnce(context.Background(), root, saved, ledger)
	if len(saved) != 1 || receiver.saveError == "" {
		t.Fatal("partial download treated as success")
	}
	files, _ = os.ReadDir(filepath.Join(root, "文件"))
	if len(files) != 1 {
		t.Fatal("partial file leaked")
	}
	receiver.peers[sender.shelf.ID].OfflineSince = time.Now()
}
