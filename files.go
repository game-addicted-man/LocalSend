package main

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var placeMu sync.Mutex

// sanitizeFileName은 폰이 보낸 파일 이름을 Windows에서도 안전한 이름으로 바꿉니다.
func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "_")
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteRune('_')
		case unicode.Is(unicode.Cf, r): // 눈에 안 보이는 방향 제어 문자 등
			continue
		default:
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(b.String())
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	// CON, PRN, AUX, NUL, COM1~9, LPT1~9 는 Windows에서 파일 이름으로 쓸 수 없어요.
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(strings.TrimSpace(stem)) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	// 너무 긴 이름은 확장자를 살려서 자릅니다.
	const maxBytes = 180
	if len(name) > maxBytes {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		base := strings.TrimSuffix(name, ext)
		for len(base)+len(ext) > maxBytes {
			_, size := utf8.DecodeLastRuneInString(base)
			base = base[:len(base)-size]
		}
		name = strings.TrimRight(base, ". ") + ext
	}
	return name
}

// uniquePath는 같은 이름이 있으면 "사진 (1).jpg"처럼 번호를 붙입니다.
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	if !pathExistsLstat(p) {
		return p
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if !pathExistsLstat(p) {
			return p
		}
	}
}

func pathExistsLstat(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// placeFile은 다 받은 임시 파일을 최종 이름으로 옮깁니다.
func placeFile(tmpPath, dir, name string) (string, error) {
	placeMu.Lock()
	defer placeMu.Unlock()
	final := uniquePath(dir, name)
	if err := os.Rename(tmpPath, final); err != nil {
		return "", err
	}
	return final, nil
}

const partialPrefix = "~localdrop-"

// cleanupPartials는 비정상 종료로 남은 받다 만 파일을 지웁니다.
func cleanupPartials(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), partialPrefix) && strings.HasSuffix(e.Name(), ".part") {
			if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
				log.Printf("받다 만 파일 정리: %s", e.Name())
			}
		}
	}
}

var knownTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif",
	".webp": "image/webp", ".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif",
	".bmp": "image/bmp", ".svg": "image/svg+xml", ".ico": "image/x-icon", ".tif": "image/tiff", ".tiff": "image/tiff",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
	".mkv": "video/x-matroska", ".avi": "video/x-msvideo", ".3gp": "video/3gpp",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".wav": "audio/wav",
	".ogg": "audio/ogg", ".flac": "audio/flac", ".amr": "audio/amr",
	".pdf": "application/pdf", ".txt": "text/plain; charset=utf-8", ".csv": "text/csv; charset=utf-8",
	".zip": "application/zip", ".apk": "application/vnd.android.package-archive",
	".doc": "application/msword", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls": "application/vnd.ms-excel", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt": "application/vnd.ms-powerpoint", ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".hwp": "application/x-hwp", ".hwpx": "application/haansofthwpx",
}

func mimeByName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := knownTypes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// 브라우저 안에서 바로 보여줘도 안전한 형식 (스크립트가 실행될 수 있는 svg/html은 제외)
func inlineSafe(mimeType string) bool {
	if mimeType == "image/svg+xml" {
		return false
	}
	return strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "video/") ||
		strings.HasPrefix(mimeType, "audio/") || mimeType == "application/pdf"
}

// contentDisposition은 한글 파일 이름이 깨지지 않도록 RFC 6266 형식으로 만듭니다.
func contentDisposition(disposition, name string) string {
	var ascii strings.Builder
	for _, r := range name {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' && r != '%' {
			ascii.WriteRune(r)
		} else {
			ascii.WriteRune('_')
		}
	}
	fallback := ascii.String()
	if strings.Trim(fallback, "_. ") == "" {
		fallback = "file" + filepath.Ext(fallback)
	}
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, fallback, rfc5987Escape(name))
}

// rfc5987Escape는 RFC 5987의 attr-char 외에는 모두 %XX로 바꿉니다.
func rfc5987Escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// serveFolderZip은 폴더를 압축 없이(zip store) 바로 흘려보냅니다.
func serveFolderZip(w http.ResponseWriter, root, name string) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name+".zip"))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	zw := zip.NewWriter(w)
	defer zw.Close()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(filepath.Join(name, rel))
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_, err := zw.CreateHeader(&zip.FileHeader{Name: rel + "/", Modified: info.ModTime(), Method: zip.Store, Flags: 0x800})
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		hdr := &zip.FileHeader{Name: rel, Modified: info.ModTime(), Method: zip.Store}
		hdr.SetMode(info.Mode())
		hdr.Flags |= 0x800 // 이름을 UTF-8로 표시 (한글 이름 깨짐 방지)
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		return err
	})
}

func folderSize(root string) int64 {
	var total int64
	deadline := time.Now().Add(2 * time.Second) // 아주 큰 폴더에서 멈추지 않도록
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || time.Now().After(deadline) {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
