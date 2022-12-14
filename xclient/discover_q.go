package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type QRegistryDiscover struct {
	*MultiServersDiscovery
	registry   string
	timeout    time.Duration
	lastUpdate time.Time
}

const defaultUpdateTimeout = time.Second * 10

func NewQRegistryDiscover(registerAddr string, timeout time.Duration) *QRegistryDiscover {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}

	d := &QRegistryDiscover{
		MultiServersDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:              registerAddr,
		timeout:               timeout,
	}

	return d
}

func (d *QRegistryDiscover) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

// Refresh 实现超时逻辑
func (d *QRegistryDiscover) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 没有超时
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}

	// 超时刷新
	log.Println("rpc registry: refresh servers from registry ", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Println("rpc registry error: ", err)
	}

	servers := strings.Split(resp.Header.Get("X-QRPC-Servers"), ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

func (d *QRegistryDiscover) Get(mode SelectMode) (string, error) {
	// 在获取前要刷新
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(mode)
}

func (d *QRegistryDiscover) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll()
}
