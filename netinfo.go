package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// Address는 폰이 접속할 수 있는 PC의 주소 후보입니다.
type Address struct {
	IP      string `json:"ip"`
	Iface   string `json:"iface"`
	URL     string `json:"url"`
	Primary bool   `json:"primary"`
	score   int
}

var virtualIfaceHints = []string{
	"vethernet", "virtualbox", "vmware", "hyper-v", "wsl", "docker", "loopback", "bluetooth",
	"블루투스", "tap-", "tap ", "tun", "npcap", "zerotier", "tailscale", "hamachi", "vpn",
	"pseudo", "teredo", "isatap", "utun", "vboxnet", "virbr", "br-", "veth", "llw", "awdl",
}

var physicalIfaceHints = []string{"wi-fi", "wifi", "wlan", "wireless", "무선", "ethernet", "이더넷", "en0", "en1", "eth", "wlp", "enp"}

// defaultRouteIP는 인터넷으로 나가는 기본 경로의 내 IP를 알아냅니다. (UDP라 실제로 보내지는 않아요)
func defaultRouteIP() net.IP {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	if a, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return a.IP
	}
	return nil
}

func isPrivateV4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	switch {
	case ip4[0] == 10:
		return true
	case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
		return true
	case ip4[0] == 192 && ip4[1] == 168:
		return true
	}
	return false
}

func lanAddresses() []Address {
	def := defaultRouteIP()
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Address
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		lname := strings.ToLower(ifc.Name)
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			score := 0
			if def != nil && def.Equal(ip) {
				score += 100
			}
			switch {
			case ip[0] == 192 && ip[1] == 168:
				score += 50
			case ip[0] == 10:
				score += 40
			case ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31:
				score += 30
			case ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127: // CGNAT, 보통 VPN
				score -= 80
			default:
				score -= 20
			}
			for _, h := range virtualIfaceHints {
				if strings.Contains(lname, h) {
					score -= 200
					break
				}
			}
			for _, h := range physicalIfaceHints {
				if strings.Contains(lname, h) {
					score += 20
					break
				}
			}
			out = append(out, Address{IP: s, Iface: ifc.Name, score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > 0 {
		out[0].Primary = true
	}
	return out
}

func (a *App) addresses() []Address {
	addrs := lanAddresses()
	for i := range addrs {
		addrs[i].URL = a.phoneURL(addrs[i].IP)
	}
	return addrs
}

func (a *App) phoneURL(ip string) string {
	a.mu.Lock()
	tok := a.cfg.Token
	a.mu.Unlock()
	host := ip
	if strings.Contains(ip, ":") {
		host = "[" + ip + "]"
	}
	return fmt.Sprintf("http://%s:%d/?t=%s", host, a.port, tok)
}

func isLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
