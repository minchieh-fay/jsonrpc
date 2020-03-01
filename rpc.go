package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

type Rpc struct {
	svc *service
}

func (rpc *Rpc) Attach(conn io.ReadWriteCloser, userParam interface{}) (*Link, error) {
	if rpc.svc == nil {
		return nil, errors.New("1|svc is nil, please run Rpc.Register first")
	}
	link := &Link{
		rpc:       rpc,
		UserParam: userParam,
		conn:      conn,
		dec:       json.NewDecoder(conn),
		enc:       json.NewEncoder(conn),
	}
	link.sendSeq = 0
	link.pending = make(map[uint64]*jRequest)
	return link, nil
}

func (rpc *Rpc) Register(rcvr interface{}) (err error) {
	if reflect.TypeOf(rcvr).Kind() != reflect.Ptr || reflect.TypeOf(rcvr).Elem().Kind() != reflect.Struct {
		return errors.New("jsonrpc: Register rcvr kind error")
	}
	rpc.svc = nil
	s := new(service)
	// load func to methods, load sub-struct(who name is _) to svcs
	if err = s.load(rcvr); err == nil {
		rpc.svc = s
	}
	fmt.Printf("===================register over==================\n")
	s.dump(0, "")
	fmt.Printf("==================================================\n")
	return
}

func CreateRpc() *Rpc {
	rpc := &Rpc{}
	return rpc
}

func DebugEnable(able bool) {
	debugable = able
}
