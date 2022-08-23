package qrpc

import (
	"QRPC/codec"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Call struct {
	Seq           uint64
	ServiceMethod string // service.method
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call
}

func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex
	mu       sync.Mutex
	header   codec.Header
	seq      uint64
	pending  map[uint64]*Call
	closing  bool // 用户将该RPC请求关闭
	shutdown bool // 服务端告诉我们该RPC关闭
}

// Client 继承于io.Client，并实现它的方法
var _ io.Closer = (*Client)(nil)

// ErrShutdown 自定义error信息
var ErrShutdown = errors.New("connection is shut down")

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

// IsAvailable return true if the client does work
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()

	return !client.shutdown && !client.closing
}

// registerCall 注册请求
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}

	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// removeCall 移除指定call，并返回
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()

	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

// terminateCalls 服务端或客户端发送错误时调用，终止该client下的所有等待执行的call请求
func (client *Client) terminateCalls(err error) {
	// 锁住发送的rpc
	client.sending.Lock()
	defer client.sending.Unlock()

	client.mu.Lock()
	client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

// receive 接受server返回的RPC调用结果
func (client *Client) receive() {
	var err error
	for {
		// 1. 解析请求头
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}

		// 2. 解析请求体
		call := client.removeCall(h.Seq) // 移除正在处理的请求
		switch {
		case call == nil:
			// call为空一般是请求不完整，或者因为其他原因取消了
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// 服务端请求出错误
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			err = client.cc.ReadBody(call.Reply) // 读取call请求中的请求体reply
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}

	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid f type %s", opt.CodecType)
		log.Println("rpc client: codec error ", err)
		return nil, err
	}

	// seding options with server
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error ", err)
		_ = conn.Close()
		return nil, err
	}

	// 前置参数处理后，开始真正处理
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		cc:      cc,
		opt:     opt,
		seq:     1, // seq从1开始，0意味着invalid call
		pending: make(map[uint64]*Call),
	}

	go client.receive()
	return client
}

// parseOptions 预处理Options
func parseOptions(opts ...*Option) (*Option, error) {
	// If opt is nil OR nil is parameter, use default options
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}

	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}

	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		// In case of null parameter error
		opt.CodecType = DefaultOption.CodecType
	}

	return opt, nil
}

// Dial_WithoutTimeout Deprecated
func Dial_WithoutTimeout(network string, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	// 使用net包进行处理
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	// 关闭连接处理
	defer func() {
		if client == nil {
			_ = client.Close()
		}
	}()

	return NewClient(conn, opt)
}

// send 发送请求
func (client *Client) send(call *Call) {
	// make sure the client send complate request
	client.sending.Lock()
	defer client.sending.Unlock()

	// 1. 注册Call请求
	seq, err := client.registerCall(call)
	if err != nil {
		// 终止请求
		call.Error = err
		call.done()
		return
	}

	// 2. 准备请求头
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = call.Seq
	client.header.Error = ""

	// 3. 编码与发送请求
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		// 错误处理
		call := client.removeCall(seq)
		// call有可能为空，这一般表示 codec 的write方法部分出错了。但是client发送了响应并处理请求
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go 异步接口，返回call实例
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call Deprecated 同步接口，封装Go方法，阻塞call.Done，等待响应返回
func (client *Client) Call_old(serviceMethod string, args, reply interface{}) error {
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}

// Call 带有超出处理的call请求方法
func (client *Client) Call(ctx context.Context, serviceMethod string, arg, reply interface{}) error {
	call := client.Go(serviceMethod, arg, reply, make(chan *Call, 1))

	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

type clientResult struct {
	client *Client
	err    error
}

// 标记函数 —— 用于函数形参简化
type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

// dialTimeout 超时处理
func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	// 如果client为空，则关闭连接
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()

	// 如果不设定超时限制，则立即阻塞等待ch返回
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}

	// 等待connectTimeout后，如果ch没有任何返回，则执行第一行的命令。
	// 否则，ch在指定限期内，返回连接的client结果，则执行成功
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect whith %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

func Dial(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewClient, network, address, opts...)
}

// NewHTTPClient 通过HTTP协议创建 client
func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	// 建立 CONNECT 连接
	// 注意！！！建立连接的字符串一个字符都不能出错！！！多了一个点都会报错！
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	// 在切换到RPC协议前，需要返回成功连接http的响应
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}

	if err == nil {
		// status != connected
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}

	return nil, err
}

// DialHTTP 通过特定的地址，连接一个HTTP RPC server
func DialHTTP(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// XDial 统一接口
func XDial(rpcAddr string, opts ...*Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}

	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		// tcp, unix or other transpoet protocol
		return Dial(protocol, addr, opts...)
	}
}
