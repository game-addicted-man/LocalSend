package main

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrSVG는 QR 코드를 선명한 SVG로 만듭니다 (화면 크기에 상관없이 깨지지 않아요).
func qrSVG(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	n := len(bm)
	var path strings.Builder
	for y, row := range bm {
		x := 0
		for x < len(row) {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#10201a" d="%s"/></svg>`, n, n, n, n, path.String()), nil
}
