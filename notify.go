package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ncruces/zenity"
)

// notifier는 폰에서 받은 것을 모아서 알림 한 번으로 보여줍니다.
// (사진 20장을 보내도 알림이 20번 뜨지 않게)
type notifier struct {
	a      *App
	mu     sync.Mutex
	files  map[string]int
	texts  map[string]int
	copied bool
	timer  *time.Timer
}

func newNotifier(a *App) *notifier {
	return &notifier{a: a, files: map[string]int{}, texts: map[string]int{}}
}

func (n *notifier) enabled() bool {
	n.a.mu.Lock()
	on := n.a.cfg.Notify
	n.a.mu.Unlock()
	if !on || *flagHeadless {
		return false
	}
	// LocalDrop 창을 보고 있으면 알림은 필요 없어요.
	if f := n.a.hooks().isForeground; f != nil && f() {
		return false
	}
	return true
}

func (n *notifier) fileReceived(device string) { n.add(device, true, false) }

func (n *notifier) textReceived(device string, copied bool) { n.add(device, false, copied) }

func (n *notifier) add(device string, file, copied bool) {
	if !n.enabled() {
		return
	}
	if device == "" {
		device = "휴대폰"
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if file {
		n.files[device]++
	} else {
		n.texts[device]++
		n.copied = n.copied || copied
	}
	if n.timer != nil {
		n.timer.Stop()
	}
	n.timer = time.AfterFunc(1500*time.Millisecond, n.fire)
}

func (n *notifier) fire() {
	n.mu.Lock()
	files, texts, copied := n.files, n.texts, n.copied
	n.files, n.texts, n.copied = map[string]int{}, map[string]int{}, false
	n.mu.Unlock()

	devs := map[string]bool{}
	nf, nt := 0, 0
	for d, c := range files {
		devs[d] = true
		nf += c
	}
	for d, c := range texts {
		devs[d] = true
		nt += c
	}
	names := make([]string, 0, len(devs))
	for d := range devs {
		names = append(names, d)
	}
	sort.Strings(names)
	who := strings.Join(names, ", ")

	var parts []string
	if nf > 0 {
		parts = append(parts, fmt.Sprintf("파일 %d개", nf))
	}
	if nt > 0 {
		parts = append(parts, fmt.Sprintf("글 %d개", nt))
	}
	msg := fmt.Sprintf("%s에서 %s를 받았어요.", who, strings.Join(parts, "와 "))
	if nf == 0 && nt == 1 && copied {
		msg = fmt.Sprintf("%s에서 보낸 글을 클립보드에 복사했어요. Ctrl+V로 붙여 넣으세요.", who)
	}
	if err := zenity.Notify(msg, zenity.Title("LocalDrop")); err != nil {
		log.Printf("알림 표시 실패: %v", err)
	}
}
