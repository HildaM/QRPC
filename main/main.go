package main

import (
	qrpc "QRPC"
	"QRPC/codec"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

func startServer(addr chan string) {
	// 选择一个可以使用的端口
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error: ", err)
	}

	log.Println("start rpc server on ", l.Addr())
	addr <- l.Addr().String()
	qrpc.Accept(l)
}

func main() {
	addr := make(chan string)
	go startServer(addr)

	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	time.Sleep(time.Second)

	// 发送 option
	_ = json.NewEncoder(conn).Encode(qrpc.DefaultOption)
	cc := codec.NewGobCodec(conn)

	for i := 0; i < 500; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("qrpc req %d", h.Seq))
		_ = cc.ReadHeader(h)

		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply: ", reply)
	}
}
