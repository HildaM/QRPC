package main

import (
	qrpc "QRPC"
	"QRPC/registry"
	"QRPC/xclient"
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}

func startRegistry(wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":9999")
	registry.HandleHTTP()
	wg.Done()
	_ = http.Serve(l, nil)
}

func startServer(registryAddr string, wg *sync.WaitGroup) {
	var foo Foo
	l, _ := net.Listen("tcp", ":0")
	server := qrpc.NewServer()

	_ = server.Register(&foo)
	registry.Heartbeat(registryAddr, "tcp@"+l.Addr().String(), 0)

	wg.Done()
	server.Accept(l)
}

// 统一打印日志
func foo(xc *xclient.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var reply int
	var err error

	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
	}

	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

// call 调用单个服务
func call(registry string) {
	d := xclient.NewQRegistryDiscover(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i ^ i})
			// foo.sleep抛开延迟，与Sum在call方法里面的调用没有区别。故不再测试
		}(i)
	}
	wg.Wait()
}

// broadcast 调用所有服务
func broadcast(registry string) {
	d := xclient.NewQRegistryDiscover(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "broadcast", "Foo.Sum", &Args{Num1: i, Num2: i ^ i})

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
			defer cancel()

			foo(xc, ctx, "broadcast", "Foo.Sleep", &Args{Num1: i, Num2: i ^ i})
		}(i)
	}
	wg.Wait()
}

func main() {
	log.SetFlags(0)

	registryAddr := "http://localhost:9999/_qrpc_/registry"
	var wg sync.WaitGroup
	wg.Add(1)

	// 确保注册中心先启动
	go startRegistry(&wg)
	wg.Wait()

	time.Sleep(time.Second)

	wg.Add(2)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()

	time.Sleep(time.Second)
	call(registryAddr)
	broadcast(registryAddr)
}
