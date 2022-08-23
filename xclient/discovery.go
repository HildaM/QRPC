package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// SelectMode 轮询模式
// TODO 暂时只实现 Random 和 RoundRobin
type SelectMode int

const (
	RandomSelect SelectMode = iota
	RoundRobinSelect
)

type Discovery interface {
	Refresh() error                      // 从注册中心更新服务列表
	Update(servers []string) error       // 手动更新服务列表
	Get(mode SelectMode) (string, error) // 根据指定轮询规则，选择一个服务实例
	GetAll() ([]string, error)           // 获取全部服务实例
}

type MultiServersDiscovery struct {
	r       *rand.Rand // 随机数
	mu      sync.Mutex
	servers []string // 服务
	index   int      // 记录当前轮询的位置
}

func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery {
	// 手动注入服务
	d := &MultiServersDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

var _ Discovery = (*MultiServersDiscovery)(nil)

func (d *MultiServersDiscovery) Refresh() error {
	return nil
}

func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.servers = servers
	return nil
}

func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}

	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers, d.servers) // 不要通过值传递，这样会将MultiServersDiscovery内部参数暴露，产生bug
	return servers, nil
}
