package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// QRegistry 简单的注册中心结构体。
// 功能：
//  1. 添加服务器		2. 接受心跳包		3. 返回所有可用服务器	4. 删除不可用服务器
type QRegistry struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*ServerItem
}

type ServerItem struct {
	Addr  string
	start time.Time
}

const (
	defaultPath    = "/_qrpc_/registry"
	defaultTimeout = time.Minute * 5
)

func New(timeout time.Duration) *QRegistry {
	return &QRegistry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

var DefaultRegistry = New(defaultTimeout)

func (r *QRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s := r.servers[addr]
	if s == nil {
		r.servers[addr] = &ServerItem{
			Addr:  addr,
			start: time.Now(),
		}
	} else {
		s.start = time.Now() // 更新时间
	}
}

func (r *QRegistry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var alives []string
	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			// Add returns the time t+d.
			// startTime+timout > time.Now   ---->  可用
			alives = append(alives, addr)
		} else {
			delete(r.servers, addr)
		}
	}

	sort.Strings(alives)
	return alives
}

func (r *QRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		w.Header().Set("X-QRPC-Servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		addr := req.Header.Get("X-QRPC-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *QRegistry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path: ", registryPath)
}

func HandleHTTP() {
	DefaultRegistry.HandleHTTP(defaultPath)
}

func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		// 确保有充足的时间发送心跳包，在服务器从注册中心中删除之前
		duration = defaultTimeout - time.Duration(1)*time.Minute // 默认 defaultTimeout - 1 = 4min
	}

	var err error
	err = sendHeartbeat(registry, addr)
	go func() {
		t := time.NewTicker(duration) // 计时器
		defer t.Stop()                // 记得释放内存！

		for err == nil {
			// 每隔duration，就会发送chan，解除阻塞  ——  相当于每个duration发送一次心跳
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

func sendHeartbeat(registry string, addr string) error {
	log.Println(addr, " send heart beat to registry ", registry)

	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-QRPC-Server", addr)
	// 发送心跳包
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server:heart beat err: ", err)
		return err
	}

	return nil
}
