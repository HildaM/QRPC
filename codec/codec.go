package codec

import "io"

type Header struct {
	ServiceMethod string // Service.method 服务名+方法名
	Seq           uint64 // 请求序号，用以区分不同请求
	Error         string // 错误信息
}

// 消息体编解码接口
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodecFunc func(io.ReadWriteCloser) Codec
type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
)

// 映射不同的编解码器
var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
