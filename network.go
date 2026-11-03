package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var netDialer = net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}

type announcement struct {
	Protocol string `json:"protocol"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Reply    bool   `json:"reply"`
}
type Catalog struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Items   []Item `json:"items"`
}

type networkInterface struct {
	Index int      `json:"index"`
	Name  string   `json:"name"`
	CIDRs []string `json:"cidrs"`
}

func (a *App) interfaces() []networkInterface {
	if data, err := os.ReadFile(filepath.Join(a.dir, "interfaces.json")); err == nil {
		var result []networkInterface
		if json.Unmarshal(data, &result) == nil {
			return result
		}
	}
	result := []networkInterface{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		entry := networkInterface{Index: iface.Index, Name: iface.Name}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			entry.CIDRs = append(entry.CIDRs, address.String())
		}
		result = append(result, entry)
	}
	return result
}

func (a *App) remember(id, name, address string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id == "" || id == a.shelf.ID {
		return
	}
	p := a.peers[id]
	if p == nil {
		p = &Peer{ID: id, Name: name, Items: []Item{}}
		a.peers[id] = p
	}
	p.Name = name
	for _, old := range p.Addresses {
		if old == address {
			return
		}
	}
	p.Addresses = append([]string{address}, p.Addresses...)
	if len(p.Addresses) > 12 {
		p.Addresses = p.Addresses[:12]
	}
}
func (a *App) discover(ctx context.Context) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 48771})
	if err != nil {
		log.Print("discovery: ", err)
		return
	}
	defer conn.Close()
	go func() { <-ctx.Done(); conn.Close() }()
	receive := func(c *net.UDPConn) {
		buf := make([]byte, 4096)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var msg announcement
			if json.Unmarshal(buf[:n], &msg) != nil || msg.Protocol != "linshare-go/1" || msg.Port < 1 || msg.Port > 65535 {
				continue
			}
			a.mu.RLock()
			own := a.shelf.ID
			a.mu.RUnlock()
			if msg.ID == own {
				continue
			}
			a.remember(msg.ID, msg.Name, net.JoinHostPort(from.IP.String(), strconv.Itoa(msg.Port)))
			if !msg.Reply {
				a.mu.RLock()
				reply := announcement{"linshare-go/1", a.shelf.ID, a.shelf.Name, a.port, true}
				a.mu.RUnlock()
				data, _ := json.Marshal(reply)
				_, _ = conn.WriteToUDP(data, from)
			}
		}
	}
	go receive(conn)
	sockets := map[int]*net.UDPConn{}
	defer func() {
		for _, c := range sockets {
			c.Close()
		}
	}()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		interfaces := a.interfaces()
		active := map[int]bool{}
		a.mu.RLock()
		msg := announcement{"linshare-go/1", a.shelf.ID, a.shelf.Name, a.port, false}
		a.mu.RUnlock()
		data, _ := json.Marshal(msg)
		for _, iface := range interfaces {
			active[iface.Index] = true
			if sockets[iface.Index] == nil {
				if c, e := net.ListenMulticastUDP("udp4", &net.Interface{Index: iface.Index, Name: iface.Name}, &net.UDPAddr{IP: net.IPv4(239, 87, 71, 1), Port: 48772}); e == nil {
					sockets[iface.Index] = c
					go receive(c)
				}
			}
			for _, addr := range iface.CIDRs {
				ip, n, e := net.ParseCIDR(addr)
				if e != nil || ip.To4() == nil || ip.IsLinkLocalUnicast() {
					continue
				}
				v := ip.To4()
				broadcast := make(net.IP, 4)
				for i := 0; i < 4; i++ {
					broadcast[i] = v[i] | ^n.Mask[i]
				}
				_, _ = conn.WriteToUDP(data, &net.UDPAddr{IP: broadcast, Port: 48771})
				// Binding the sender to each address also covers hotspot and wired/Wi-Fi mixtures.
				if sender, e := net.DialUDP("udp4", &net.UDPAddr{IP: ip}, &net.UDPAddr{IP: net.IPv4(239, 87, 71, 1), Port: 48772}); e == nil {
					_, _ = sender.Write(data)
					sender.Close()
				}
			}
		}
		for id, c := range sockets {
			if !active[id] {
				c.Close()
				delete(sockets, id)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		a.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) refresh(ctx context.Context) {
	if !a.refreshMu.TryLock() {
		return
	}
	defer a.refreshMu.Unlock()
	a.mu.RLock()
	peers := make([]Peer, 0, len(a.peers))
	for _, p := range a.peers {
		copy := *p
		copy.Addresses = append([]string{}, p.Addresses...)
		peers = append(peers, copy)
	}
	a.mu.RUnlock()
	limit := make(chan struct{}, 6)
	done := make(chan struct{}, len(peers))
	for _, p := range peers {
		limit <- struct{}{}
		go func(p Peer) { defer func() { <-limit; done <- struct{}{} }(); a.refreshPeer(ctx, p) }(p)
	}
	for range peers {
		<-done
	}
	a.mu.Lock()
	saved := make([]Peer, 0, len(a.peers))
	for _, p := range a.peers {
		p.expireItems(time.Now())
		saved = append(saved, *p)
	}
	if err := atomicJSON(a.dir+"/peers.json", saved); err != nil {
		log.Print(err)
	}
	a.mu.Unlock()
}
func (a *App) refreshPeer(ctx context.Context, p Peer) {
	var last error
	for _, address := range p.Addresses {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/v1/catalog", nil)
		req.Header.Set("X-Share-Code", p.Code)
		res, err := a.client.Do(req)
		if err != nil {
			last = err
			continue
		}
		var cat Catalog
		if res.StatusCode == http.StatusUnauthorized {
			res.Body.Close()
			a.mu.Lock()
			current := a.peers[p.ID]
			if current == nil {
				a.mu.Unlock()
				return
			}
			current.Locked = true
			current.Online = true
			current.OfflineSince = time.Time{}
			current.Error = "需要共享码"
			current.Items = []Item{}
			a.mu.Unlock()
			return
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			last = fmt.Errorf("HTTP %d", res.StatusCode)
			continue
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&cat)
		res.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if cat.ID != p.ID {
			last = fmt.Errorf("设备标识已变化")
			continue
		}
		if cat.Items == nil {
			cat.Items = []Item{}
		}
		a.mu.Lock()
		current := a.peers[p.ID]
		if current == nil {
			a.mu.Unlock()
			return
		}
		// Try the last reachable address first after switching between networks.
		for i, candidate := range current.Addresses {
			if candidate == address {
				current.Addresses[0], current.Addresses[i] = current.Addresses[i], current.Addresses[0]
				break
			}
		}
		current.Items = cat.Items
		current.Name = cat.Name
		current.Online = true
		current.OfflineSince = time.Time{}
		current.Enabled = cat.Enabled
		current.Locked = false
		current.Updated = time.Now()
		current.Error = ""
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.peers[p.ID]
	if current == nil {
		return
	}
	if current.Online || current.OfflineSince.IsZero() {
		current.OfflineSince = time.Now()
	}
	current.Online = false
	current.Locked = false
	current.Error = "设备离线 · 缓存即将自动清理"
	current.expireItems(time.Now())
	if last != nil {
		log.Printf("peer %s: %v", p.ID, last)
	}
}
func (a *App) connect(address, code string) error {
	address = strings.TrimSpace(strings.TrimPrefix(address, "http://"))
	address = strings.TrimRight(address, "/")
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "48771")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil {
		return fmt.Errorf("请输入设备的局域网 IP 地址")
	}
	req, _ := http.NewRequest("GET", "http://"+address+"/v1/info", nil)
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("连接失败：%d", res.StatusCode)
	}
	var cat Catalog
	if err = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&cat); err != nil {
		return err
	}
	a.mu.RLock()
	own := a.shelf.ID
	a.mu.RUnlock()
	if cat.ID == own {
		return fmt.Errorf("这是本机地址")
	}
	a.remember(cat.ID, cat.Name, address)
	a.mu.Lock()
	a.peers[cat.ID].Code = code
	p := *a.peers[cat.ID]
	p.Addresses = append([]string{}, p.Addresses...)
	a.mu.Unlock()
	a.refreshPeer(context.Background(), p)
	return nil
}
func (a *App) localAddresses() []string {
	result := []string{}
	ifaces := a.interfaces()
	for _, i := range ifaces {
		for _, address := range i.CIDRs {
			ip, _, e := net.ParseCIDR(address)
			if e == nil && ip.To4() != nil && !ip.IsLinkLocalUnicast() {
				result = append(result, net.JoinHostPort(ip.String(), strconv.Itoa(a.port)))
			}
		}
	}
	sort.Strings(result)
	return result
}
