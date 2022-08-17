package codec

type Header struct {
	ServiceMethod string // Service.method 服务名+方法名
	Seq           uint64 // 请求序号，用以区分不同请求
	Error         string // 错误信息
}
