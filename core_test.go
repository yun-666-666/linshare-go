package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := openApp(t.TempDir(), "test-device")
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func TestShelfSurvivesRefreshOutageAndRestart(t *testing.T) {
	sender := newTestApp(t)
	receiver := newTestApp(t)
	text, err := sender.add("", "", "刷新后仍能看到这段文字", nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := sender.add("example.txt", "text/plain", "", strings.NewReader("original file contents"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(sender.publicHandler())
	address := strings.TrimPrefix(server.URL, "http://")
	if err := receiver.connect(address, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		receiver.refresh(context.Background())
		if len(receiver.peers[sender.shelf.ID].Items) != 2 {
			t.Fatal("refresh lost content")
		}
	}
	request := httptest.NewRequest("GET", server.URL+"/v1/files/"+file.ID, nil)
	request.Header.Set("Range", "bytes=0-7")
	response := httptest.NewRecorder()
	sender.publicHandler().ServeHTTP(response, request)
	if response.Code != 206 || response.Body.String() != "original" {
		t.Fatalf("range transfer: %d %q", response.Code, response.Body.String())
	}
	server.Close()
	receiver.refresh(context.Background())
	peer := receiver.peers[sender.shelf.ID]
	if peer.Online || len(peer.Items) != 2 {
		t.Fatal("temporary outage erased last successful shelf")
	}
	restarted, err := openApp(receiver.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.peers[sender.shelf.ID].Items) != 2 {
		t.Fatal("cached remote shelf missing after restart")
	}
	senderAgain, err := openApp(sender.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(senderAgain.shelf.Items) != 2 || senderAgain.shelf.Items[0].Text != text.Text {
		t.Fatal("local shelf missing after restart")
	}
	if err = senderAgain.remove(text.ID); err != nil {
		t.Fatal(err)
	}
	if err = senderAgain.remove(file.ID); err != nil {
		t.Fatal(err)
	}
	live := httptest.NewServer(senderAgain.publicHandler())
	defer live.Close()
	restarted.peers[sender.shelf.ID].Addresses = []string{strings.TrimPrefix(live.URL, "http://")}
	restarted.refresh(context.Background())
	if len(restarted.peers[sender.shelf.ID].Items) != 0 {
		t.Fatal("successful empty catalog must remove stale items")
	}
}
func TestBrokenRefreshDoesNotEraseAndPauseIsPersistent(t *testing.T) {
	sender := newTestApp(t)
	receiver := newTestApp(t)
	_, _ = sender.add("", "", "retained", nil)
	broken := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken && r.URL.Path == "/v1/catalog" {
			io.WriteString(w, `{"items":`)
			return
		}
		sender.publicHandler().ServeHTTP(w, r)
	}))
	defer server.Close()
	if err := receiver.connect(strings.TrimPrefix(server.URL, "http://"), ""); err != nil {
		t.Fatal(err)
	}
	broken = true
	receiver.refresh(context.Background())
	if len(receiver.peers[sender.shelf.ID].Items) != 1 {
		t.Fatal("malformed refresh erased content")
	}
	broken = false
	sender.shelf.Enabled = false
	if err := sender.persist(sender.shelf); err != nil {
		t.Fatal(err)
	}
	receiver.refresh(context.Background())
	if len(receiver.peers[sender.shelf.ID].Items) != 0 {
		t.Fatal("paused shelf remains published")
	}
	restarted, err := openApp(sender.dir, "")
	if err != nil || restarted.shelf.Enabled || len(restarted.shelf.Items) != 1 {
		t.Fatal("pause did not persist content")
	}
}
func TestCodeAndImportThroughAPI(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("POST", "http://127.0.0.1:48773/api/text", strings.NewReader(`{"text":"中文输入直接发布"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.localHandler().ServeHTTP(w, req)
	if w.Code != 200 || len(a.shelf.Items) != 1 {
		t.Fatalf("text publish failed %d %s", w.Code, w.Body.String())
	}
	a.shelf.Code = "1234"
	w = httptest.NewRecorder()
	a.publicHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://peer/v1/catalog", nil))
	if w.Code != 401 {
		t.Fatal("missing code accepted")
	}
	req = httptest.NewRequest("GET", "http://peer/v1/catalog", nil)
	req.Header.Set("X-Share-Code", "1234")
	w = httptest.NewRecorder()
	a.publicHandler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "中文输入直接发布") {
		t.Fatal("valid code rejected")
	}
}
