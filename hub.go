package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Hub는 열려 있는 화면들(PC 화면, 폰 화면)에 실시간 알림(Server-Sent Events)을 보냅니다.
type Hub struct {
	mu      sync.Mutex
	clients map[*sseClient]struct{}
	closed  bool
}

type sseClient struct {
	ch    chan sseMsg
	done  chan struct{}
	admin bool
	token string // 폰이 연결할 때 쓴 비밀 코드 (연결 초기화 시 끊기 위함)
	once  sync.Once
}

type sseMsg struct {
	event string
	data  []byte
}

func newHub() *Hub { return &Hub{clients: map[*sseClient]struct{}{}} }

func (c *sseClient) close() { c.once.Do(func() { close(c.done) }) }

func (h *Hub) add(admin bool, token string) *sseClient {
	c := &sseClient{ch: make(chan sseMsg, 128), done: make(chan struct{}), admin: admin, token: token}
	h.mu.Lock()
	if h.closed {
		c.close()
	} else {
		h.clients[c] = struct{}{}
	}
	h.mu.Unlock()
	return c
}

func (h *Hub) remove(c *sseClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.close()
}

func (h *Hub) send(event string, v any, toAdmin, toPhone bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.deliver(func(admin bool) []byte { return b }, event, toAdmin, toPhone)
}

// sendFunc는 받는 쪽(PC/폰)에 따라 다른 내용을 보낼 때 씁니다.
func (h *Hub) sendFunc(event string, f func(admin bool) any, toAdmin, toPhone bool) {
	var adminB, phoneB []byte
	if toAdmin {
		adminB, _ = json.Marshal(f(true))
	}
	if toPhone {
		phoneB, _ = json.Marshal(f(false))
	}
	h.deliver(func(admin bool) []byte {
		if admin {
			return adminB
		}
		return phoneB
	}, event, toAdmin, toPhone)
}

func (h *Hub) deliver(data func(admin bool) []byte, event string, toAdmin, toPhone bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if (c.admin && !toAdmin) || (!c.admin && !toPhone) {
			continue
		}
		select {
		case c.ch <- sseMsg{event: event, data: data(c.admin)}:
		default:
			// 너무 밀린 화면은 끊습니다. 다시 연결하면서 전체 상태를 새로 받아요.
			delete(h.clients, c)
			c.close()
		}
	}
}

// kickPhones는 연결 초기화 때 예전 코드로 연결된 폰을 모두 끊습니다.
func (h *Hub) kickPhones(currentToken string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.admin || c.token == currentToken {
			continue
		}
		select {
		case c.ch <- sseMsg{event: "reset", data: []byte("{}")}:
		default:
		}
		delete(h.clients, c)
		go func(c *sseClient) {
			time.Sleep(300 * time.Millisecond) // reset 메시지가 전달될 시간을 줍니다
			c.close()
		}(c)
	}
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		delete(h.clients, c)
		c.close()
	}
}

// serveSSE는 연결을 열어 두고 이벤트를 흘려보냅니다.
func serveSSE(w http.ResponseWriter, r *http.Request, c *sseClient, hello any) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	write := func(event string, data []byte) bool {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	fmt.Fprint(w, "retry: 2000\n\n")
	hb, _ := json.Marshal(hello)
	if !write("hello", hb) {
		return
	}

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.done:
			// 남아 있는 메시지(예: reset)를 마저 보내고 끝냅니다.
			for {
				select {
				case m := <-c.ch:
					write(m.event, m.data)
				default:
					return
				}
			}
		case m := <-c.ch:
			if !write(m.event, m.data) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
