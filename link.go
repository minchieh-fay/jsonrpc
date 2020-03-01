package jsonrpc

import (
	"encoding/json"
	"io"
	"sync"
)

type Link struct {
	UserParam interface{}
	//seq       uint64
	//pending   map[uint64]*Call

	conn io.ReadWriteCloser
	dec  *json.Decoder // for reading JSON values
	enc  *json.Encoder // for writing JSON values
	rpc  *Rpc

	// send request
	sendSeq       uint64
	pending_mutex sync.Mutex
	pending       map[uint64]*jRequest
}

func (link *Link) Request(method string, params interface{}, result interface{}) error {
	jreq := new(jRequest)
	link.pending_mutex.Lock()
	jreq.seq = link.sendSeq
	link.sendSeq++
	link.pending[jreq.seq] = jreq
	link.pending_mutex.Unlock()

	jreq.jrpc.Id = &jreq.seq
	jreq.jrpc.Method = method
	jreq.jrpc.Params = params
	jreq.result = result
	jreq.done = make(chan *jRequest, 1)
	link.enc.Encode(jreq.jrpc)
	jreq = <-jreq.done
	return jreq.jrpc.Error
}

func (link *Link) Notify(method string, notify interface{}) {
	jrpc := jRPC{
		Method: method,
		Params: notify,
	}
	link.enc.Encode(jrpc)
}

// make a jsonrpc error
func (link *Link) Error(code int64, message string) error {
	return &jError{
		Code:    code,
		Message: message,
	}
}

func (link *Link) ErrorInfo(err error) (int64, string) {
	return err.(*jError).Code, err.(*jError).Message
}

func (link *Link) Run() error {
	for {
		msg := getMsg()
		if err := link.dec.Decode(msg); err != nil {
			return err
		}
		go link.doMsg(msg)
	}
}
