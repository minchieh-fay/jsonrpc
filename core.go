package jsonrpc

import (
	"encoding/json"
	"fmt"
	"go/token"
	"log"
	"reflect"
	"strings"
	"sync"

	jsoniter "github.com/json-iterator/go"
)

var typeOfError = reflect.TypeOf((*error)(nil)).Elem()
var debugable = false

func pp(format string, v ...interface{}) {
	if debugable {
		log.Printf("jsonrpc: "+format, v...)
	}
}

var freeMsgQ *Msg
var msgLock sync.Mutex

type Msg struct {
	Method string           `json:"method"`
	Params *json.RawMessage `json:"params"`
	Id     *uint64          `json:"id"`
	Result *json.RawMessage `json:"result"`
	Error  *json.RawMessage `json:"error"`
	next   *Msg
}

func getMsg() *Msg {
	msgLock.Lock()
	msg := freeMsgQ
	if msg == nil {
		msg = new(Msg)
	} else {
		freeMsgQ = msg.next
		*msg = Msg{}
	}
	msgLock.Unlock()
	return msg
}

func freeMsg(msg *Msg) {
	msgLock.Lock()
	msg.next = freeMsgQ
	freeMsgQ = msg
	msgLock.Unlock()
}

func (link *Link) sendErrorRsponse(id *uint64, code int64, message string) {
	jrpc := jRPC{
		Id:    id,
		Error: link.Error(code, message).(*jError),
	}
	link.enc.Encode(jrpc)
}

func (jreq *jRequest) Done() {
	select {
	case jreq.done <- jreq:
		// ok
	default:
		// We don't want to block here. It is the caller's responsibility to make
		// sure the channel has enough buffer space. See comment in Go().
		if debugable {
			log.Println("rpc: discarding Call reply due to insufficient Done chan capacity")
		}
	}
}

var json_iterator = jsoniter.ConfigCompatibleWithStandardLibrary

func (link *Link) doMsg(msg *Msg) {
	if msg.Id == nil && msg.Method == "" {
		link.sendErrorRsponse(nil, -32600, "Invalid Request")
	}

	if msg.Method == "" { // response
		seq := *msg.Id
		link.pending_mutex.Lock()
		jreq := link.pending[seq]
		delete(link.pending, seq)
		link.pending_mutex.Unlock()
		if jreq == nil {
			panic("err, jreq is lost")
		}
		if msg.Result != nil {
			json_iterator.Unmarshal(*msg.Result, jreq.result)
		}
		if msg.Error != nil {
			jreq.jrpc.Error = new(jError)
			json_iterator.Unmarshal(*msg.Error, jreq.jrpc.Error)
		}
		jreq.Done()
		return
	} else { // request || notify
		units := strings.Split(msg.Method, ".")
		len := len(units)
		strMethod := units[len-1]
		svc := link.rpc.svc
		for i := 0; i < len-1; i++ {
			svc = svc.svcs[strings.ToUpper(units[i])]
			if svc == nil {
				link.sendErrorRsponse(msg.Id, -32601, "Method not found")
				return
			}
		}
		mtype := svc.methods[strings.ToUpper(strMethod)]
		if mtype == nil {
			link.sendErrorRsponse(msg.Id, -32601, "Method not found")
			return
		}

		var params reflect.Value
		{ // 取jsonrpc中的params数据
			paramsIsValue := false
			if mtype.Params.Kind() == reflect.Ptr {
				params = reflect.New(mtype.Params.Elem())
			} else {
				params = reflect.New(mtype.Params)
				paramsIsValue = true
			}
			json_iterator.Unmarshal(*msg.Params, params.Interface())
			if paramsIsValue {
				params = params.Elem()
			}
		}
		if msg.Id == nil { // notify
			if mtype.Result != nil { // 注册的函数原型中有result, 不符合notify
				link.sendErrorRsponse(nil, -32600, "Invalid Request")
			}
			mtype.method.Func.Call([]reflect.Value{svc.rcvr, reflect.ValueOf(link), params})
			return
		} else { // request
			if mtype.Result == nil { // 注册的函数原型中没有result, 不符合request
				link.sendErrorRsponse(nil, -32600, "Invalid Request")
				return
			}
			var result reflect.Value
			{ // 按注册的函数原型, 初始化result对象
				if mtype.Result.Kind() == reflect.Ptr {
					result = reflect.New(mtype.Result.Elem())
					switch mtype.Result.Elem().Kind() {
					case reflect.Map:
						result.Elem().Set(reflect.MakeMap(mtype.Result.Elem()))
					case reflect.Slice:
						result.Elem().Set(reflect.MakeSlice(mtype.Result.Elem(), 0, 0))
					}
				} else {
					result = reflect.New(mtype.Result)
					result = result.Elem()
				}
			}
			{ // rpc调用并回复
				returnValues := mtype.method.Func.Call([]reflect.Value{svc.rcvr, reflect.ValueOf(link), params, result})
				errInter := returnValues[0].Interface()
				if errInter != nil {
					jrpc := jRPC{
						Id:    msg.Id,
						Error: errInter.(*jError),
					}
					link.enc.Encode(jrpc)
					return
				}
				// success response
				jrpc := jRPC{
					Id:     msg.Id,
					Result: result.Interface(),
				}
				link.enc.Encode(jrpc)
			}
			return
		}
	}

	// request
}

type methodType struct {
	sync.Mutex // protects counters
	method     reflect.Method
	Params     reflect.Type
	Result     reflect.Type
	numCalls   uint
}

type service struct {
	name    string                 // 服务的名字, 一般为`T`
	rcvr    reflect.Value          // 方法的接受者, 即约定中的 `t`
	typ     reflect.Type           // 注册的类型, 即约定中的`T`
	methods map[string]*methodType // 注册的方法, 即约定中的`MethodName`的集合
	svcs    map[string]*service
}

func (s *service) load(rcvr interface{}) error {
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()

	s.methods = suitableMethods(s.typ)
	s.svcs = suitableStructs(s.typ.Elem(), s.rcvr.Elem())

	return nil
}

const DUMPTAB string = "  "

func (s *service) dump(tab int, name string) error {
	// step.1  打印当前服务的名称
	for i := 0; i < tab; i++ {
		//fmt.Printf("\t")
		fmt.Printf(DUMPTAB)
	}
	if tab == 0 {
		fmt.Printf("*                  %s                 *\n", s.name)
	} else {
		fmt.Printf("- %s\n", name)
	}

	// step.2  打印当前服务的方法
	for key := range s.methods {
		for i := 0; i < tab+1; i++ {
			fmt.Printf(DUMPTAB)
		}
		fmt.Printf("%s\n", key)
	}

	// step.2  打印当前服务的子服务信息
	for key, value := range s.svcs {
		value.dump(tab+1, key)
	}

	return nil
}

// Is this type exported or a builtin?
func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return token.IsExported(t.Name()) || t.PkgPath() == ""
}

func suitableMethods(typ reflect.Type) map[string]*methodType {
	pp("[%q] suitableMethods, %q\n", typ.Elem().Name(), typ.NumMethod())
	methods := make(map[string]*methodType)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		typ := method.Type
		name := method.Name
		// Method must be exported.
		if method.PkgPath != "" {
			pp("pkgpath=%q\n", method.PkgPath)
			continue
		}
		// request ins=4, notify ins=3
		if typ.NumIn() != 4 && typ.NumIn() != 3 {
			pp("NumIn=%q\n", typ.NumIn())
			continue
		}
		// First arg need not be *Link.
		linkType := typ.In(1)
		if linkType.Kind() != reflect.Ptr || linkType.Elem().Kind() != reflect.Struct ||
			linkType.Elem().Name() != "Link" {
			pp("frist param=%q\n", linkType)
			continue
		}

		paramsType := typ.In(2)

		if typ.NumIn() == 4 {
			resultType := typ.In(3)

			// Method needs one out.
			if typ.NumOut() != 1 {
				continue
			}
			// The return type of the method must be error.
			if returnType := typ.Out(0); returnType != typeOfError {
				continue
			}
			pp("[install method]request %q, %q\n", name, typ.In(3))
			methods[strings.ToUpper(name)] = &methodType{method: method, Params: paramsType, Result: resultType}
		} else if typ.NumIn() == 3 {
			// Method needs 0 out.
			if typ.NumOut() != 0 {
				continue
			}
			pp("[install method]notify %q\n", name)
			methods[strings.ToUpper(name)] = &methodType{method: method, Params: paramsType, Result: nil}
		}

	}
	return methods
}

func suitableStructs(typ reflect.Type, rcvr reflect.Value) map[string]*service {
	structs := make(map[string]*service)
	for m := 0; m < typ.NumField(); m++ {
		if rcvr.Field(m).Type().Kind() == reflect.Struct {
			s := new(service)
			if err := s.load(rcvr.Field(m).Addr().Interface()); err == nil {
				structs[strings.ToUpper(typ.Field(m).Name)] = s
			}
		}
	}
	return structs
}
