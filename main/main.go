package main

import (
	qrpc "QRPC"
	"log"
	"net"
	"sync"
	"time"
)

func startServer(addr chan string) {
	// 1. 注册服务
	var foo Foo
	if err := qrpc.Register(&foo); err != nil {
		log.Fatal("register error ", err)
	}

	// 2. 选择端口
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error ", err)
	}

	log.Println("start rpc server on ", l.Addr())
	addr <- l.Addr().String()
	qrpc.Accept(l)
}

func main() {
	log.SetFlags(0)

	addr := make(chan string)
	go startServer(addr)

	client, _ := qrpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{
				Num1: i,
				Num2: i * 100,
			}
			var reply int
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error: ", err)
			}

			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}

	wg.Wait()
}

type Foo int
type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}
