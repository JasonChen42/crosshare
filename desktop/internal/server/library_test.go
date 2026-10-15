package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/crosshare/desktop/internal/storage"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLibraryRoundTripAndAuthorization(t *testing.T) {
	s, d, db := testUploadServer(t)
	s.cfg.PairedDevices = append(s.cfg.PairedDevices, *d)
	other := *d
	other.DeviceID = "other"
	other.Token = "other-token"
	s.cfg.PairedDevices = append(s.cfg.PairedDevices, other)
	img := image.NewRGBA(image.Rect(0, 0, 12, 8))
	img.Set(3, 4, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	call := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, bytes.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("X-CrossHare-UI", "1")
		r.Header.Set("X-CrossHare-File-Name-Base64", base64.StdEncoding.EncodeToString([]byte("photo.png")))
		w := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(w, r)
		return w
	}
	imported := call("POST", "/api/ui/library/import", "", buf.Bytes())
	if imported.Code != 200 {
		t.Fatal(imported.Body.String())
	}
	var result struct {
		Hash string `json:"hash"`
	}
	json.Unmarshal(imported.Body.Bytes(), &result)
	if r := call("GET", "/api/library", "", nil); r.Code != 401 {
		t.Fatalf("anonymous catalogue: %d", r.Code)
	}
	if r := call("GET", "/api/library/asset/"+result.Hash, "token", nil); r.Code != 200 || !bytes.Equal(r.Body.Bytes(), buf.Bytes()) {
		t.Fatalf("download mismatch: %d", r.Code)
	}
	if r := call("GET", "/api/library/thumbnail/"+result.Hash, "token", nil); r.Code != 200 || r.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumbnail: %d %s", r.Code, r.Body.String())
	}
	body := []byte(fmt.Sprintf(`{"hash":%q,"device_id":"phone-1"}`, result.Hash))
	if r := call("POST", "/api/ui/library/send", "", body); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	tasks, err := db.Deliveries(d.DeviceID)
	if err != nil || len(tasks) != 1 || tasks[0].Status != "pending" {
		t.Fatalf("tasks %+v %v", tasks, err)
	}
	path := fmt.Sprintf("/api/library/transfers/%d/ack", tasks[0].ID)
	if r := call("POST", path, "other-token", []byte(`{}`)); r.Code != 404 {
		t.Fatalf("cross-device ack: %d", r.Code)
	}
	if r := call("POST", path, "token", []byte(`{"error":"disk full"}`)); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	tasks, _ = db.Deliveries(d.DeviceID)
	if tasks[0].Status != "pending" || tasks[0].Error != "disk full" {
		t.Fatalf("failed delivery falsely complete: %+v", tasks)
	}
	if r := call("POST", path, "token", []byte(`{}`)); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	tasks, _ = db.Deliveries(d.DeviceID)
	if tasks[0].Status != "done" {
		t.Fatal("ack not recorded")
	}
	if r := call("POST", "/api/ui/library/import", "", buf.Bytes()); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	count, _, _ := db.GetSyncStats()
	if count != 1 {
		t.Fatalf("duplicate imported: %d", count)
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/library/asset/"+result.Hash, nil)
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Range", "bytes=5-9")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, r)
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), buf.Bytes()[5:10]) {
		t.Fatalf("range: %d", w.Code)
	}
}
func TestLibraryRejectsCrossOriginMutations(t *testing.T) {
	s, _, _ := testUploadServer(t)
	for _, test := range []struct{ host, origin, header, peer string }{
		{"evil.example", "", "1", "127.0.0.1:3"},
		{"127.0.0.1", "http://evil.example", "1", "127.0.0.1:3"},
		{"127.0.0.1", "", "", "127.0.0.1:3"},
		{"127.0.0.1", "", "1", "192.168.1.2:3"},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://"+test.host+"/api/ui/library/import", bytes.NewBufferString("invalid"))
		r.RemoteAddr = test.peer
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-CrossHare-UI", test.header)
		w := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%+v got %d", test, w.Code)
		}
	}
}

func TestLibraryPaginationHasExactTotalsAndBounds(t *testing.T) {
	s, _, db := testUploadServer(t)
	for i := 0; i < 100; i++ {
		_, err := db.AddSyncedFile(&storage.SyncedFile{ContentHash: fmt.Sprintf("%064x", i), OriginalName: fmt.Sprintf("photo-%03d.jpg", i), DateTaken: time.Unix(int64(i), 0), LocalPath: "/unused", MediaType: "photo", DeviceID: "desktop"})
		if err != nil {
			t.Fatal(err)
		}
	}
	call := func(q string) ([]storage.LibraryItem, int, bool) {
		r := httptest.NewRequest("GET", "http://127.0.0.1/api/ui/library"+q, nil)
		r.RemoteAddr = "127.0.0.1:4"
		w := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var body struct {
			Items []storage.LibraryItem `json:"items"`
			Total int                   `json:"total"`
			More  bool                  `json:"has_more"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Items, body.Total, body.More
	}
	first, total, more := call("?limit=48")
	if len(first) != 48 || total != 100 || !more {
		t.Fatalf("first: %d %d %v", len(first), total, more)
	}
	second, _, _ := call("?limit=48&offset=48")
	if len(second) != 48 || first[47].Hash == second[0].Hash {
		t.Fatal("page overlap")
	}
	last, total, more := call("?limit=48&offset=96")
	if len(last) != 4 || total != 100 || more {
		t.Fatal("incorrect last page")
	}
	capped, _, _ := call("?limit=999999")
	if len(capped) != 96 {
		t.Fatal("unbounded page")
	}
	filtered, total, _ := call("?q=photo-099")
	if len(filtered) != 1 || total != 1 {
		t.Fatal("filtered total mismatch")
	}
}

func TestImportIntoAlbumAndWholeAlbumAuthorization(t *testing.T) {
	s, d, db := testUploadServer(t)
	s.cfg.PairedDevices = append(s.cfg.PairedDevices, *d)
	a, _ := db.CreateAlbum("folder-a")
	b, _ := db.CreateAlbum("folder-b")
	var pngData bytes.Buffer
	png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	for _, id := range []int64{a, b, a} {
		r := httptest.NewRequest("POST", fmt.Sprintf("http://127.0.0.1/api/ui/library/import?album_id=%d", id), bytes.NewReader(pngData.Bytes()))
		r.RemoteAddr = "127.0.0.1:9"
		r.Header.Set("X-CrossHare-UI", "1")
		r.Header.Set("X-CrossHare-File-Name-Base64", base64.StdEncoding.EncodeToString([]byte("one.png")))
		w := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	albums, _ := db.Albums()
	for _, a := range albums {
		if a.Count != 1 {
			t.Fatalf("failed duplicate import membership %+v", a)
		}
	}
	count, _, _ := db.GetSyncStats()
	if count != 1 {
		t.Fatal("import duplicated original")
	}
	path := fmt.Sprintf("http://127.0.0.1/api/library/albums/%d/send", a)
	anon := httptest.NewRequest("POST", path, bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, anon)
	if w.Code != 401 {
		t.Fatal("anonymous bulk send accepted")
	}
	req := httptest.NewRequest("POST", path, bytes.NewBufferString(`{"device_id":"someone-else"}`))
	req.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	tasks, _ := db.Deliveries("phone-1")
	if len(tasks) != 1 || tasks[0].AlbumID != a {
		t.Fatal("wrong bulk target")
	}
}
