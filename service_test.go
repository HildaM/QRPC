package qrpc

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

// 测试可导出方法Sum，非导出方法sum
func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}
func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

// 自定义断言函数
func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

func TestNewService(t *testing.T) {
	var foo Foo

	s := newService(&foo)
	_assert(len(s.method) == 1, "wrong service Method, expect 1, but got %d", len(s.method))

	// 可导出
	mType := s.method["Sum"]
	_assert(mType != nil, "wrong Method, method shouldn't nil")

	// 不可导出
	mType1 := s.method["sum"]
	_assert(mType1 != nil, "wrong Method, method shouldn't nil")
}

func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.method["Sum"]
	//mType1 := s.method["sum"]
	//mTypeErr := s.method["Error"]

	// Normal test
	argv := mType.newArgv()
	replyv := mType.newReplyv()
	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 10}))

	err := s.call(mType, argv, replyv)
	_assert(err == nil && *replyv.Interface().(*int) == 11 && mType.NumCalls() == 1,
		"failed to call Foo.Sum")

	//// Unexported method test
	//argv1 := mType1.newArgv()
	//replyv1 := mType1.newReplyv()
	//argv1.Set(reflect.ValueOf(Args{Num1: 1, Num2: 10}))
	//
	//err = s.call(mType1, argv1, replyv1)
	//if !(err == nil && *replyv1.Interface().(*int) == 11 && mType1.NumCalls() == 1) {
	//	log.Println(err)
	//}
	//
	//// Error method test
	//argvErr := mTypeErr.newArgv()
	//replyvErr := mTypeErr.newReplyv()
	//argvErr.Set(reflect.ValueOf(Args{Num1: 1, Num2: 10}))
	//
	//err = s.call(mTypeErr, argvErr, replyvErr)
	//if !(err == nil && *replyvErr.Interface().(*int) == 11 && mTypeErr.NumCalls() == 1) {
	//	log.Println(err)
	//}
}
