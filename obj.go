package jsonrpc

type jError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func (e *jError) Error() string {
	return e.Message
}

type jParams interface{}
type jResult interface{}

type jRPC struct {
	Method string  `json:"method,omitempty"`
	Params jParams `json:"params,omitempty"`
	Result jResult `json:"result,omitempty"`
	Id     *uint64 `json:"id,omitempty"`
	Error  *jError `json:"error,omitempty"`
}

type jRequest struct {
	jrpc   jRPC
	seq    uint64
	result interface{}    // remote response
	done   chan *jRequest // Strobes when call is complete.
}
