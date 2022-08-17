package qrpc

import (
	"QRPC/codec"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int        // 用来标记当前的请求是 RPC请求
	CodecType   codec.Type // 设定当前请求body的类型
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct{}

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
		log.Panicf("rcp server: invalid magic number %x, may be it's not a rpc request", opt.MagicNumber)
		return
	}

	// 2. 确定解析方式
	dec := codec.NewCodecFuncMap[opt.CodecType]
	if dec == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	// 3. 处理请求
	server.serverCodec(dec(conn))
}

// 错误请求，为了后续出现RPC请求失败而设置
var invalidRequest = struct{}{}

/*
1. 读取请求 readRequest
2. 处理请求 handleRequest
3. 回复请求 sendResponse
*/
func (server *Server) serverCodec(cc codec.Codec) {
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
		// 使用协程并非处理请求
		go server.handleRequest(cc, req, sending, wg)
	}

	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h            *codec.Header // 请求头
	argv, replyv reflect.Value // 请求体与响应体（使用反射确定）
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	req := &request{
		h: h,
	}

	// TODO 暂时不处理请求的参数类型，日后处理
	req.argv = reflect.New(reflect.TypeOf("")) // 以空参数替代
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argc error: ", err)
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

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// TODO 需要请求注册的RPC方法来获取正确的请求

	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("QRPC resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}
