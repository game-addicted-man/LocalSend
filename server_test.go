package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"IMG_0001.JPG":                    "IMG_0001.JPG",
		"../../etc/passwd":                "passwd",
		`..\..\Windows\win.ini`:           "win.ini",
		"a<b>c:d\"e|f?g*h.txt":            "a_b_c_d_e_f_g_h.txt",
		"CON":                             "_CON",
		"nul.txt":                         "_nul.txt",
		"  spaced name.  ":                "spaced name",
		"":                                "file",
		"..":                              "file",
		"사진 2024.jpg":                     "사진 2024.jpg",
		"tab\there\x00.png":               "tabhere.png",
		"‮gnp.exe":                        "gnp.exe",
		strings.Repeat("가", 100) + ".jpg": strings.Repeat("가", 58) + ".jpg",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	p1 := uniquePath(dir, "a.jpg")
	os.WriteFile(p1, []byte("x"), 0o644)
	p2 := uniquePath(dir, "a.jpg")
	if filepath.Base(p2) != "a (1).jpg" {
		t.Fatalf("got %s", p2)
	}
	os.WriteFile(p2, []byte("x"), 0o644)
	if p3 := uniquePath(dir, "a.jpg"); filepath.Base(p3) != "a (2).jpg" {
		t.Fatalf("got %s", p3)
	}
}

func TestContentDisposition(t *testing.T) {
	got := contentDisposition("attachment", "견적서 (최종).pdf")
	want := `attachment; filename="___ (__).pdf"; filename*=UTF-8''%EA%B2%AC%EC%A0%81%EC%84%9C%20%28%EC%B5%9C%EC%A2%85%29.pdf`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// testApp은 임시 폴더를 쓰는 앱과 테스트 서버를 만듭니다.
func testApp(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "cache"))
	a, err := newApp(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	a.cfg.SaveDir = filepath.Join(tmp, "recv")
	a.cfg.Notify = false
	a.cfg.AutoCopy = false
	srv := httptest.NewServer(a.routes())
	t.Cleanup(func() {
		a.hub.closeAll()
		srv.Close()
	})
	return a, srv
}

func phoneReq(t *testing.T, a *App, method, url string, body io.Reader) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	req.Header.Set("X-LD", "1")
	req.Header.Set("X-LD-Token", a.cfg.Token)
	req.Header.Set("X-LD-Device-Id", "dev1")
	req.Header.Set("X-LD-Device", "%ED%85%8C%EC%8A%A4%ED%8A%B8%ED%8F%B0") // 테스트폰
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPhoneFlow(t *testing.T) {
	a, srv := testApp(t)

	// SSE 연결
	evReq, _ := http.NewRequest("GET", srv.URL+"/api/events?t="+a.cfg.Token+"&d=dev1&n=x", nil)
	evRes, err := http.DefaultClient.Do(evReq)
	if err != nil {
		t.Fatal(err)
	}
	defer evRes.Body.Close()
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(evRes.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "event: ") {
				events <- strings.TrimPrefix(line, "event: ")
			}
		}
	}()
	expect := func(name string) {
		t.Helper()
		select {
		case got := <-events:
			if got != name {
				t.Fatalf("event %q, want %q", got, name)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %q event", name)
		}
	}
	expect("hello")

	// 파일 보내기
	data := bytes.Repeat([]byte("localdrop"), 50000)
	res := phoneReq(t, a, "POST", srv.URL+"/api/upload?name=%EC%82%AC%EC%A7%84.jpg&size=450000&cid=c1", bytes.NewReader(data))
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("upload status %d %s", res.StatusCode, b)
	}
	var item ItemView
	json.NewDecoder(res.Body).Decode(&item)
	res.Body.Close()
	if item.Name != "사진.jpg" || item.Size != int64(len(data)) || item.Device != "테스트폰" || item.CID != "c1" || item.Path != "" {
		t.Fatalf("unexpected item %+v", item)
	}
	got, err := os.ReadFile(filepath.Join(a.cfg.SaveDir, "사진.jpg"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("saved file mismatch: %v", err)
	}
	expect("item")

	// 크기가 안 맞으면(중간에 끊김) 저장하지 않음
	res = phoneReq(t, a, "POST", srv.URL+"/api/upload?name=cut.bin&size=999999", bytes.NewReader([]byte("short")))
	if res.StatusCode != 400 {
		t.Fatalf("short upload status %d", res.StatusCode)
	}
	res.Body.Close()
	if _, err := os.Stat(filepath.Join(a.cfg.SaveDir, "cut.bin")); err == nil {
		t.Fatal("incomplete file should not be saved")
	}
	entries, _ := os.ReadDir(a.cfg.SaveDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), partialPrefix) {
			t.Fatalf("partial left behind: %s", e.Name())
		}
	}

	// 글 보내기
	res = phoneReq(t, a, "POST", srv.URL+"/api/text", strings.NewReader(`{"text":"안녕 https://example.com","cid":"c2"}`))
	if res.StatusCode != 200 {
		t.Fatalf("text status %d", res.StatusCode)
	}
	res.Body.Close()
	expect("item")

	// 상태
	res = phoneReq(t, a, "GET", srv.URL+"/api/state", nil)
	var st struct {
		Items []ItemView `json:"items"`
	}
	json.NewDecoder(res.Body).Decode(&st)
	res.Body.Close()
	if len(st.Items) != 2 {
		t.Fatalf("items = %d", len(st.Items))
	}

	// 다운로드 + Range
	req, _ := http.NewRequest("GET", srv.URL+"/api/file/"+item.ID+"?t="+a.cfg.Token, nil)
	req.Header.Set("Range", "bytes=0-8")
	res, _ = http.DefaultClient.Do(req)
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(b) != "localdrop" {
		t.Fatalf("range: %d %q", res.StatusCode, b)
	}

	// 연결 초기화하면 예전 코드는 막힘
	old := a.cfg.Token
	areq, _ := http.NewRequest("POST", srv.URL+"/api/pc/reset", nil)
	areq.Header.Set("X-LD-Admin", a.adminToken)
	res, _ = http.DefaultClient.Do(areq)
	res.Body.Close()
	if res.StatusCode != 200 || a.currentToken() == old {
		t.Fatalf("reset failed: %d", res.StatusCode)
	}
	expect("reset")
	res, _ = http.Get(srv.URL + "/api/state?t=" + old)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("old token still works: %d", res.StatusCode)
	}
}

func TestAdminProtection(t *testing.T) {
	a, srv := testApp(t)
	h := a.routes()

	// 같은 PC + 토큰 → 허용
	req := httptest.NewRequest("GET", "http://127.0.0.1/api/pc/state", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("X-LD-Admin", a.adminToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("local admin: %d", rec.Code)
	}

	// 같은 와이파이의 다른 기기 → 토큰이 있어도 거절
	req = httptest.NewRequest("GET", "http://192.168.0.10/api/pc/state", nil)
	req.RemoteAddr = "192.168.0.23:5000"
	req.Header.Set("X-LD-Admin", a.adminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("LAN admin should be refused: %d", rec.Code)
	}

	// Host가 다른 이름(DNS 리바인딩) → 거절
	req = httptest.NewRequest("GET", "http://evil.test/api/pc/state", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("X-LD-Admin", a.adminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("rebinding should be refused: %d", rec.Code)
	}

	// 쿠키만으로 바꾸는 요청 → 거절 (헤더 필요)
	req, _ = http.NewRequest("POST", srv.URL+"/api/pc/clear", nil)
	req.AddCookie(&http.Cookie{Name: "ld_a", Value: a.adminToken})
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cookie-only mutation should be refused: %d", res.StatusCode)
	}

	// 폰 경로에 토큰 없이 → 401
	res, _ = http.Get(srv.URL + "/api/state")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("no token: %d", res.StatusCode)
	}
}
