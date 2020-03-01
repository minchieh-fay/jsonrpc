# jsonrpc



## 定位

附加到用户指定的句柄(socket/file/pipe...)上，进行jsonrpc操作，严格遵守jsonrpc2规范的简易高性能操作栈



## 和官方josnrpc区别 

除了以下区别， 其他与官网的jsonrpc一致：

1.我们不强制要求用户输入的params和resut必须是结构体指针，int和string这种jsonrpc2.0官方允许的裸数据可以直接作为params和result

2.在流式处理IO处和官方相同的实用了官方的encode/json， 而在具体解析用户数据（params和result）的子数据时使用了全宇宙速度最快的jsonitor（从官网和实测上都表现为encode/json的6-7倍），这在用户发送/接受大数据时表现优异，性能可以与protobuffer比肩

3.我们没有像官网一样强制区分client和server，官网定义只有client可以发送request，只有server可以处理请求，我们不定性用户的身份，有数据就分发到用户注册的函数/对象中，有请求就帮助发出去，遵照jsonrpc2.0定义

4.我们不提供像官网的dial，listen等接口，这些链接生命周期的东西全部交给用户自行处理，用户想用tcp，udp，pipe，file，shell等都可以，将读写句柄让我们attach上就可以

5.函数回调时，我们会带上用户当时attach该链路时带上的自定义的一个interface指针，方便用户使用

6.我们任何端都可以接受notify消息，只要用户注册了该notify的处理函数

7.json格式错误怎么办，我们遵守jsonrpc2.0规范进行审查关键字段（id，method，result，error），出现错误格式，协议栈会直接回复对方错误，错误数据不会进入用户代码，错误码和错误信息全部符合jsonrpc2.0定义



## jsonrpc的技术定义

* rpc------这个是和链接/实际交互无关的对象，从他派生出link对象。可以在rpc中注册处理函数/对象
* link------由rpc派生出来，他和链路1:1绑定， 回调rpc处理方法/发送请求(或notify)



## 使用方法

**jsonrpc注册的方法中的params和result都可以是结构体，为了演示简单，我这边都用了int**

* 注册函数(单层)

  ```go
  type Toolbox struct{}
  func (*Toolbox)Hello(link* jsonrpc.Link, params int, result int*) error{
  	log.Println("i receive ", params, "i will send +1 ")
  	*result = params + 1
  	return nil
  }
  rpc := jsonrpc.CreateRpc()
  rpc.Register(new(Toolbox))
  ----------------------------------
  => {"method":"Hello","params":1, "id":0}
  <= {"id":0, "result":2}
  ```

  备注： 第一层（Toolbox）不会出现在method中, 如果要注册notify函数，定义的函数用前2个参数，删掉result即可

  函数名称大小写无关

  注意：函数首字母得大写，不然反问不到（golang的约束）

  

* 注册函数(多层)

  ```go
  type Toolbox struct{}
  type ToolboxFather struct{
    Son Toolbox
  }
  func (*Toolbox)Hello(link* jsonrpc.Link, params int, result int*) error{
  	log.Println("i receive ", params, "i will send +1 ")
  	*result = params + 1
  	return nil
  }
  rpc := jsonrpc.CreateRpc()
  rpc.Register(new(ToolboxFather))
  ----------------------------------
  => {"method":"son.Hello","params":1, "id":0}
  <= {"id":0, "result":2}
  ```

  备注：这里演示了2层，你可以无限的通过子结构体放置不限层数的函数定义

  注意：子结构体变量名的首字母得大写，不然反问不到（golang的约束）

  

* 启动

  ```go
  rpc := jsonrpc.CreateRpc()
  rpc.Register(new(xxx))
  ```

  这样其实jsonrpc已经启动了

  

* 挂载到链路上

  ```go
  func onHandle(ws *websocket.Conn) {// websocket的accept
    link, err := rpc.Attach(ws, nil) // rpc由jsonrpc.CreateRpc()创建，可以全局使用一个，第二个参数是用户自定义数据，比如某个客户端登陆，可以把user指针存进去，之后可以在link指针中取到该指针
  	if err != nil {
  		panic(err)
  	}
  	link.Run() // 协程会在这里卡住，直到conn断开（被动断开或者用户手动close了该conn），run之后，收到jsonrpc消息会自动回调用户register的函数
  }
  ```

  备注：这个link指针可以存下来，他下面有Notify和Request函数供给用户主动发送请求/通知

  ​          不区分客户端服务端，如果客户端也是有conn的，交给jsonrpc库attach即可

  ​			我这边用了websocket，其实可以用tcp，udp等其他具备可读可写句柄的链路，都可以attach



## Sample

```go
// 发送请求
var a int
err := link.Request("hello", 1, &a)
if err != nil {
  code, message := link.ErrorInfo(err)
  log.Println("failed:", code, message)
  return
}
log.Println("i recv result:", a)
```

```go
// 发送通知
link.Notify("hello", 1)
```

```go
// 接受请求, 
func (*XXX)Hello(link* jsonrpc.Link, params int, result *int) error{
  log.Println("该链路我存入的数据：", link.UserParam)
  if params < 1 {
    return link.Error(11, "params must greater than 1")
  }
  result = params + 1
  return nil
}
```

```go
// 接受notify
func (*XXX)Hello(link* jsonrpc.Link, params int) {
  log.Println("该链路我存入的数据：", link.UserParam)
  log.Println("i recv:", params)
}
```

