package main

import (
	qrpc "QRPC"
	"fmt"
	"log"
	"net"
	"sync"
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
	log.SetFlags(0)

	// 1. 初始化数据
	addr := make(chan string)
	go startServer(addr)

	client, _ := qrpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)

	// 2. RPC请求
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			args := fmt.Sprintf("qrpc req %d", i)
			var reply string

			if err := client.Call("Foo.sum", args, &reply); err != nil {
				log.Fatal("call Foo.sum error: ", err)
			}
			log.Println("reply: ", reply)
		}(i)
	}
	wg.Wait()

	// 3. 测试异步请求
	args := "Asynchronous Call"
	var reply string
	done := make(chan *qrpc.Call, 10)
	call := client.Go("Foo.sum", args, &reply, done)

	println(call)
}
