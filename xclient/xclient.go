package xclient

import (
	. "QRPC"
	"context"
	"io"
	"reflect"
	"sync"
)

type XClient struct {
	d       Discovery
	mode    SelectMode
	mu      sync.Mutex
	opt     *Option
	clients map[string]*Client
}

var _ io.Closer = (*XClient)(nil)

func NewXClient(d Discovery, mode SelectMode, opt *Option) *XClient {
	return &XClient{
		d:       d,
		mode:    mode,
		opt:     opt,
		clients: make(map[string]*Client),
	}
}

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key) // 关闭后记得清理内存！
	}

	return nil
}

// dial 返回已存在的client或者创建新的client
func (xc *XClient) dial(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}

	// 一开始找不到client、或者client已经不可用。手动创建
	if client == nil {
		var err error
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}

		xc.clients[rpcAddr] = client
	}

	return client, nil
}

// call 根据rpcAddr获取client，并调用该client的Call方法处理请求
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return nil
	}

	return client.Call(ctx, serviceMethod, args, reply)
}

// Call 从注册中心获取可以使用的主机IP，然后再向该指定IP发起调用
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}

	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// Broadcast 广播请求
// 任何一个实例发生错误，只返回一个错误；如果调用成功，不管数量多少，只返回一个结果
// 并发请求，以提升性能
// 需要使用互斥锁保证 error 和 reply 被正确赋值
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var e error
	replyDone := reply == nil              // reply为空，则不需要设置
	ctx, cancel := context.WithCancel(ctx) // 保证错误出现时，快速失败
	defer cancel()

	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()

			var clonedReply interface{}
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}

			err := xc.call(rpcAddr, ctx, serviceMethod, args, clonedReply)

			// 确保只赋值一次
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel()
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()

		}(rpcAddr)
	}

	wg.Wait()
	return e
}
