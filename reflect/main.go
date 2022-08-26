package main

import (
	"log"
	"reflect"
	"strings"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	typ := reflect.TypeOf(&wg)

	// 通过反射遍历wg下的所有方法
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		argv := make([]string, 0, method.Type.NumIn()) // make([]string, len, cap)
		returns := make([]string, 0, method.Type.NumIn())

		// j从 1 开始，第 0 个入参是 wg 自己
		for j := 1; j < method.Type.NumIn(); j++ {
			argv = append(argv, method.Type.In(j).Name())
		}
		for j := 0; j < method.Type.NumOut(); j++ {
			returns = append(returns, method.Type.Out(j).Name())
		}

		log.Printf("func (w *%s) %s(%s) %s",
			typ.Elem().Name(),
			method.Name,
			strings.Join(argv, ","), // 用逗号分割
			strings.Join(returns, ","),
		)
	}

	// 测试ticker
	for {
		t := time.NewTicker(time.Duration(1) * time.Minute)
		<-t.C

		println(time.Now().Minute())
	}
}
