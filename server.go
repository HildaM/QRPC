package qrpc

import (
	"QRPC/codec"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber    int           // 用来标记当前的请求是 RPC请求
	CodecType      codec.Type    // 设定当前请求body的类型
	ConnectTimeout time.Duration // 0 意味着没有限制
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10, // 默认10秒
}

type Server struct {
	serviceMap sync.Map // 并发读写map
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// 创建链接 —— 接受来自listener中的每一个链接
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error: ", err)
			return
		}
		// 为每一个链接单独创建一个协程
		go server.ServerConn(conn)
	}
}

// 对外暴露的接口
func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

// 创建链接方法
func (server *Server) ServerConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()

	var opt Option
	// 1. 解析数据
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: option error: ", err)
		return
	}
	// 判断是否为RPC请求
	if opt.MagicNumber != MagicNumber {
		log.Printf("rcp server: invalid magic number %x, may be it's not a rpc request", opt.MagicNumber)
		return
	}

	// 2. 确定解析方式
	dec := codec.NewCodecFuncMap[opt.CodecType]
	if dec == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	// 3. 处理请求
	server.serverCodec(dec(conn), &opt)
}

// 错误请求，为了后续出现RPC请求失败而设置的空结构体
var invalidRequest = struct{}{}

/*
1. 读取请求 readRequest
2. 处理请求 handleRequest
3. 回复请求 sendResponse
*/
func (server *Server) serverCodec(cc codec.Codec, opt *Option) {
	// 原子操作，确保发送一个完整的请求
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)

	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				// 请求为空，无法恢复，直接关闭连接
				break
			}
			// 回信发送方，让其再次发送，以求发送正常请求数据
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		// 正常处理RPC请求
		wg.Add(1)
		// 使用协程并发处理请求
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}

	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h            *codec.Header // 请求头
	argv, replyv reflect.Value // 请求体与响应体（使用反射确定）
	mtype        *methodType
	svc          *service
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	// 1. 读取请求头，获取请求的ServiceMethod
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	// 2. 根据ServiceMethod，寻找服务
	req := &request{h: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// 需要确保请求参数是一个指针类型，readBody方法需要指针作为入参
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		// Addr returns a pointer value representing the address of v.
		argvi = req.argv.Addr().Interface() // 将值参数转换为指针参数
	}

	// 3. 解析远程函数的入参
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read argc error: ", err)
		return req, err
	}

	return req, nil
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	// 读取头
	if err := cc.ReadHeader(&h); err != nil {
		// 非文件结尾，说明出现了问题
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error: ", err)
		}
		return nil, err
	}

	return &h, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	// 确保不同请求发送不会混淆，造成处理错误
	sending.Lock()
	defer sending.Unlock()

	// 发送请求
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error: ", err)
	}
}

// handleRequest Deprecated
func (server *Server) handleRequest_old(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	// RPC远程调用方法
	if err := req.svc.call(req.mtype, req.argv, req.replyv); err != nil {
		req.h.Error = err.Error()
		server.sendResponse(cc, req.h, invalidRequest, sending)
		return
	}

	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()

	called := make(chan struct{})
	sent := make(chan struct{})

	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}

		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

// Register 将服务注册
func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		// 检测是否重复注册
		return errors.New("rpc service already defined: " + s.name)
	}
	return nil
}

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

// findService 根据serviceMethod字符串寻找已经注册的服务
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed " + serviceMethod)
		return
	}

	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}

	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}

	return
}
