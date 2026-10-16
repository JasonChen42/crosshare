package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/crosshare/desktop/internal/config"
	"github.com/crosshare/desktop/internal/media"
	"github.com/rwcarlsen/goexif/exif"
)

//go:embed web/*
var libraryWeb embed.FS

// uiRequestKey marks requests served to the local browser UI, which may see
// desktop file paths unlike the phone API.
type uiRequestKeyType struct{}

var uiRequestKey = uiRequestKeyType{}

func (s *Server) registerLibrary(mux *http.ServeMux) {
	mux.HandleFunc("GET /library-assets/{file}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		data, err := libraryWeb.ReadFile("web/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
		w.Write(data)
	})
	for _, prefix := range []string{"/api/library", "/api/ui/library"} {
		protect := s.authMiddleware
		if strings.Contains(prefix, "/ui/") {
			protect = s.libraryLocal
		}
		mux.HandleFunc("GET "+prefix, protect(s.libraryList))
		mux.HandleFunc("GET "+prefix+"/asset/{hash}", protect(s.libraryFile))
		mux.HandleFunc("GET "+prefix+"/thumbnail/{hash}", protect(s.libraryThumbnail))
		mux.HandleFunc("GET "+prefix+"/transfers", protect(s.libraryTransfers))
		mux.HandleFunc("POST "+prefix+"/send", protect(s.librarySend))
		mux.HandleFunc("POST "+prefix+"/favorite/{hash}", protect(s.libraryFavorite))
		mux.HandleFunc("GET "+prefix+"/albums", protect(s.libraryAlbums))
		mux.HandleFunc("POST "+prefix+"/albums", protect(s.libraryCreateAlbum))
		mux.HandleFunc("POST "+prefix+"/albums/{id}", protect(s.libraryAddAlbum))
		mux.HandleFunc("POST "+prefix+"/albums/{id}/send", protect(s.librarySendAlbum))
	}
	mux.HandleFunc("POST /api/library/transfers/{id}/ack", s.authMiddleware(s.libraryAck))
	mux.HandleFunc("POST /api/ui/library/import", s.libraryLocal(s.libraryImport))
	mux.HandleFunc("POST /api/ui/library/reveal/{hash}", s.libraryLocal(s.libraryReveal))
	mux.HandleFunc("GET /api/ui/library/devices", s.libraryLocal(func(w http.ResponseWriter, r *http.Request) {
		devices := []map[string]string{}
		for _, d := range s.cfg.GetPairedDevices() {
			devices = append(devices, map[string]string{"id": d.DeviceID, "name": d.DeviceName, "last_seen": d.LastSeen})
		}
		writeJSON(w, 200, map[string]any{"devices": devices, "name": s.cfg.DeviceName, "pair_code": s.cfg.GetPairCode()})
	}))
}

// Browser endpoints require a loopback Host as well as peer, and mutations require
// a non-simple header. Cross-origin pages cannot submit imports or queue sends.
func (s *Server) libraryLocal(next http.HandlerFunc) http.HandlerFunc {
	return s.localOnly(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "invalid host", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host || u.Scheme != "http" {
				http.Error(w, "invalid origin", 403)
				return
			}
		}
		if r.Method != "GET" && r.Header.Get("X-CrossHare-UI") != "1" {
			http.Error(w, "missing UI header", 403)
			return
		}
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		// Local browser responses may expose desktop file paths; phone
		// responses stay metadata-only.
		next(w, r.WithContext(context.WithValue(r.Context(), uiRequestKey, true)))
	})
}
func decodeLibrary(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid request", 400)
		return false
	}
	return true
}
func (s *Server) libraryList(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	album, _ := strconv.ParseInt(r.URL.Query().Get("album"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 48
	}
	if limit > 96 {
		limit = 96
	}
	total, err := s.db.LibraryCount(r.URL.Query().Get("q"), album, r.URL.Query().Get("favorite") == "1")
	if err != nil {
		http.Error(w, "cannot count library", 500)
		return
	}
	items, err := s.db.Library(limit, offset, r.URL.Query().Get("q"), album, r.URL.Query().Get("favorite") == "1")
	if err != nil {
		http.Error(w, "cannot read library", 500)
		return
	}
	if r.Context().Value(uiRequestKey) == nil {
		// Phones get metadata only; desktop file paths stay on this machine.
		for i := range items {
			items[i].LocalPath = ""
		}
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_offset": offset + len(items), "has_more": offset+len(items) < total, "total": total, "limit": limit})
}
func (s *Server) libraryFile(w http.ResponseWriter, r *http.Request) {
	hash, err := media.NormalizeSHA256(r.PathValue("hash"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.db.LibraryAsset(hash)
	if err != nil {
		http.Error(w, "cannot read asset", 500)
		return
	}
	if f == nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(f.LocalPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	head := make([]byte, 512)
	n, _ := file.Read(head)
	file.Seek(0, 0)
	kind := http.DetectContentType(head[:n])
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Type", kind)
	w.Header().Set("ETag", `"`+hash+`"`)
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" || (!strings.HasPrefix(kind, "image/") && !strings.HasPrefix(kind, "video/")) {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Name}))
	http.ServeContent(w, r, f.Name, info.ModTime(), file)
}
func (s *Server) libraryThumbnail(w http.ResponseWriter, r *http.Request) {
	hash, err := media.NormalizeSHA256(r.PathValue("hash"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.db.LibraryAsset(hash)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	// Serialize bounded decodes to avoid concurrent full-resolution image allocation.
	locked := false
	defer func() {
		if locked {
			s.libraryMu.Unlock()
		}
	}()
	cache := filepath.Join(config.ConfigDir(), "thumbnails")
	path := filepath.Join(cache, hash+".jpg")
	if _, err = os.Stat(path); err != nil {
		s.libraryMu.Lock()
		locked = true
		if _, e := os.Stat(path); e == nil {
			s.libraryMu.Unlock()
			locked = false
			w.Header().Set("Content-Type", "image/jpeg")
			http.ServeFile(w, r, path)
			return
		}
		source, e := os.Open(f.LocalPath)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		defer source.Close()
		orientation := 1
		if metadata, err := exif.Decode(source); err == nil {
			if tag, err := metadata.Get(exif.Orientation); err == nil {
				if value, err := tag.Int(0); err == nil {
					orientation = value
				}
			}
		}
		source.Seek(0, 0)
		cfg, _, e := image.DecodeConfig(source)
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
			http.Error(w, "preview unavailable; download original", 415)
			return
		}
		source.Seek(0, 0)
		src, _, e := image.Decode(source)
		if e != nil {
			http.Error(w, "preview unavailable", 415)
			return
		}
		orientedWidth, orientedHeight := cfg.Width, cfg.Height
		if orientation >= 5 && orientation <= 8 {
			orientedWidth, orientedHeight = orientedHeight, orientedWidth
		}
		width, height := orientedWidth, orientedHeight
		if width > 480 || height > 480 {
			if width > height {
				height = height * 480 / width
				width = 480
			} else {
				width = width * 480 / height
				height = 480
			}
		}
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		bounds := src.Bounds()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				ox, oy := x*orientedWidth/width, y*orientedHeight/height
				sx, sy := ox, oy
				switch orientation {
				case 2:
					sx = cfg.Width - 1 - ox
				case 3:
					sx, sy = cfg.Width-1-ox, cfg.Height-1-oy
				case 4:
					sy = cfg.Height - 1 - oy
				case 5:
					sx, sy = oy, ox
				case 6:
					sx, sy = oy, cfg.Height-1-ox
				case 7:
					sx, sy = cfg.Width-1-oy, cfg.Height-1-ox
				case 8:
					sx, sy = cfg.Width-1-oy, ox
				}
				dst.Set(x, y, src.At(bounds.Min.X+sx, bounds.Min.Y+sy))
			}
		}
		if e = os.MkdirAll(cache, 0700); e != nil {
			http.Error(w, "preview cache unavailable", 500)
			return
		}
		tmp, e := os.CreateTemp(cache, "thumb-*")
		if e != nil {
			http.Error(w, "preview cache unavailable", 500)
			return
		}
		defer os.Remove(tmp.Name())
		e = jpeg.Encode(tmp, dst, &jpeg.Options{Quality: 82})
		closeErr := tmp.Close()
		if e != nil || closeErr != nil {
			http.Error(w, "preview failed", 500)
			return
		}
		if e = os.Rename(tmp.Name(), path); e != nil {
			http.Error(w, "preview failed", 500)
			return
		}
	}
	if locked {
		s.libraryMu.Unlock()
		locked = false
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}
func (s *Server) libraryImport(w http.ResponseWriter, r *http.Request) {
	var albumID int64
	if value := r.URL.Query().Get("album_id"); value != "" {
		var err error
		albumID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || albumID < 1 {
			http.Error(w, "invalid album", 400)
			return
		}
		album, err := s.db.Album(albumID)
		if err != nil {
			http.Error(w, "cannot read album", 500)
			return
		}
		if album == nil {
			http.Error(w, "album not found", 404)
			return
		}
	}
	// Stream import to disk; reuse the existing verified, atomic upload pipeline.
	tmp, err := os.CreateTemp("", "crosshare-import-*")
	if err != nil {
		http.Error(w, "cannot import", 500)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, digest), http.MaxBytesReader(w, r.Body, media.MaxFileBytesFromEnv()))
	if err != nil {
		http.Error(w, "import interrupted or too large", 400)
		return
	}
	tmp.Seek(0, 0)
	head := make([]byte, 512)
	hn, _ := tmp.Read(head)
	kind := http.DetectContentType(head[:hn])
	if !strings.HasPrefix(kind, "image/") && !strings.HasPrefix(kind, "video/") {
		http.Error(w, "请选择支持的图片或视频文件", 415)
		return
	}
	tmp.Seek(0, 0)
	r.SetPathValue("hash", fmt.Sprintf("%x", digest.Sum(nil)))
	r.Header.Set("X-CrossHare-File-Size", fmt.Sprint(n))
	r.Header.Set("X-CrossHare-Media-Type", "photo")
	if strings.HasPrefix(kind, "video/") {
		r.Header.Set("X-CrossHare-Media-Type", "video")
	}
	if r.Header.Get("X-CrossHare-Date-Taken-Ms") == "" {
		r.Header.Set("X-CrossHare-Date-Taken-Ms", fmt.Sprint(time.Now().UnixMilli()))
	}
	r.Body = io.NopCloser(tmp)
	r = r.WithContext(context.WithValue(r.Context(), "device", &config.PairedDevice{DeviceID: desktopImportDeviceID, DeviceName: s.cfg.DeviceName}))
	reply := &importReply{header: make(http.Header)}
	s.handleMediaUpload(reply, r)
	if reply.status >= 200 && reply.status < 300 && albumID > 0 {
		var body struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(reply.body.Bytes(), &body); err != nil {
			http.Error(w, "cannot read import result", 500)
			return
		}
		if err := s.db.AddToAlbum(albumID, body.Hash); err != nil {
			http.Error(w, "file imported but album update failed; retry import", 500)
			return
		}
	}
	for k, v := range reply.header {
		w.Header()[k] = v
	}
	w.WriteHeader(reply.status)
	w.Write(reply.body.Bytes())
}

type importReply struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *importReply) Header() http.Header { return r.header }
func (r *importReply) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *importReply) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	return r.body.Write(data)
}

// libraryReveal shows the asset's original file in the desktop file manager.
// A browser cannot open a folder itself, so the loopback server performs the
// desktop open on its behalf.
func (s *Server) libraryReveal(w http.ResponseWriter, r *http.Request) {
	hash, err := media.NormalizeSHA256(r.PathValue("hash"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.db.LibraryAsset(hash)
	if err != nil {
		http.Error(w, "cannot read asset", 500)
		return
	}
	if f == nil || f.LocalPath == "" {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(f.LocalPath); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	name, args := revealCommand(runtime.GOOS, f.LocalPath)
	if err := s.reveal(name, args...); err != nil {
		log.Printf("[server] failed to reveal %s: %v", f.LocalPath, err)
		http.Error(w, "cannot open folder", 500)
		return
	}
	log.Printf("[server] revealed %s", f.LocalPath)
	writeJSON(w, 200, map[string]any{"ok": true, "path": f.LocalPath})
}

// revealCommand builds the platform command that shows a local file in its
// folder: Finder reveals the file itself, Explorer selects it, and the Linux
// fallback opens the containing folder.
func revealCommand(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{"-R", path}
	case "windows":
		return "explorer", []string{"/select," + path}
	default:
		return "xdg-open", []string{filepath.Dir(path)}
	}
}
func (s *Server) librarySendAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "invalid album", 400)
		return
	}
	var body struct {
		Device string `json:"device_id"`
	}
	if !decodeLibrary(w, r, &body) {
		return
	}
	if device, ok := r.Context().Value("device").(*config.PairedDevice); ok {
		body.Device = device.DeviceID
	}
	valid := false
	for _, d := range s.cfg.GetPairedDevices() {
		if d.DeviceID == body.Device {
			valid = true
		}
	}
	if !valid {
		http.Error(w, "device not paired", 400)
		return
	}
	album, err := s.db.Album(id)
	if err != nil {
		http.Error(w, "cannot read album", 500)
		return
	}
	if album == nil {
		http.NotFound(w, r)
		return
	}
	count, err := s.db.QueueAlbum(id, body.Device)
	if err != nil {
		http.Error(w, "cannot queue album", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"queued": count, "album_id": id, "album_name": album.Name})
}
func (s *Server) libraryTransfers(w http.ResponseWriter, r *http.Request) {
	device := ""
	if d, ok := r.Context().Value("device").(*config.PairedDevice); ok {
		device = d.DeviceID
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 48
	}
	if limit > 96 {
		limit = 96
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	items, err := s.db.DeliveriesPage(device, limit+1, offset, r.URL.Query().Get("pending") == "1")
	if err != nil {
		http.Error(w, "cannot read transfers", 500)
		return
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	writeJSON(w, 200, map[string]any{"items": items, "has_more": more, "next_offset": offset + len(items)})
}
func (s *Server) librarySend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hash   string `json:"hash"`
		Device string `json:"device_id"`
	}
	if !decodeLibrary(w, r, &body) {
		return
	}
	if d, ok := r.Context().Value("device").(*config.PairedDevice); ok {
		body.Device = d.DeviceID
	}
	valid := false
	for _, d := range s.cfg.GetPairedDevices() {
		if d.DeviceID == body.Device {
			valid = true
		}
	}
	if !valid {
		http.Error(w, "device not paired", 400)
		return
	}
	f, err := s.db.LibraryAsset(body.Hash)
	if err != nil {
		http.Error(w, "cannot read asset", 500)
		return
	}
	if f == nil {
		http.NotFound(w, r)
		return
	}
	if err = s.db.QueueDelivery(body.Hash, body.Device); err != nil {
		http.Error(w, "cannot queue transfer", 500)
		return
	}
	writeJSON(w, 200, map[string]bool{"queued": true})
}
func (s *Server) libraryAck(w http.ResponseWriter, r *http.Request) {
	d := r.Context().Value("device").(*config.PairedDevice)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body struct {
		Error string `json:"error"`
	}
	if !decodeLibrary(w, r, &body) {
		return
	}
	if len(body.Error) > 1000 {
		http.Error(w, "error too long", 400)
		return
	}
	ok, err := s.db.AckDelivery(id, d.DeviceID, body.Error)
	if err != nil {
		http.Error(w, "cannot acknowledge", 500)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) libraryFavorite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Favorite bool `json:"favorite"`
	}
	if !decodeLibrary(w, r, &body) {
		return
	}
	f, err := s.db.LibraryAsset(r.PathValue("hash"))
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	if err = s.db.SetFavorite(f.Hash, body.Favorite); err != nil {
		http.Error(w, "cannot update", 500)
		return
	}
	writeJSON(w, 200, body)
}
func (s *Server) libraryAlbums(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.Albums()
	if err != nil {
		http.Error(w, "cannot read albums", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"items": a})
}
func (s *Server) libraryCreateAlbum(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeLibrary(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len(body.Name) == 0 || len(body.Name) > 200 {
		http.Error(w, "invalid name", 400)
		return
	}
	id, err := s.db.CreateAlbum(body.Name)
	if err != nil {
		http.Error(w, "cannot create album", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}
func (s *Server) libraryAddAlbum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body struct {
		Hash string `json:"hash"`
	}
	if !decodeLibrary(w, r, &body) {
		return
	}
	f, err := s.db.LibraryAsset(body.Hash)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	if err = s.db.AddToAlbum(id, body.Hash); err != nil {
		http.Error(w, "album unavailable", 400)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleLibraryUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := libraryWeb.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Write(data)
}
