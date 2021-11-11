package topdown

import (
	"net"
	"sync"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

type lookupIPAddrCacheKey string

// resolv is the same as net.DefaultResolver -- this is for mocking it out in tests
var resolv = &net.Resolver{}
var mutex = sync.RWMutex{}

func mutateResolver(f func(*net.Resolver)) {
	mutex.Lock()
	f(resolv)
	mutex.Unlock()
}

func builtinLookupIPAddr(bctx BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	name, err := builtins.StringOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	key := lookupIPAddrCacheKey(name)
	if val, ok := bctx.Cache.Get(key); ok {
		return iter(val.(*ast.Term))
	}

	mutex.RLock()
	addrs, err := resolv.LookupIPAddr(bctx.Context, string(name))
	mutex.RUnlock()

	if err != nil {
		// NOTE(sr): We can't do better than this right now, see https://github.com/golang/go/issues/36208
		if err.Error() == "operation was canceled" || err.Error() == "i/o timeout" {
			return Halt{
				Err: &Error{
					Code:     CancelErr,
					Message:  ast.NetLookupIPAddr.Name + ": " + err.Error(),
					Location: bctx.Location,
				},
			}
		}
		return err
	}

	ret := ast.NewSet()
	for _, a := range addrs {
		ret.Add(ast.StringTerm(a.String()))

	}
	t := ast.NewTerm(ret)
	bctx.Cache.Put(key, t)
	return iter(t)
}

func init() {
	RegisterBuiltinFunc(ast.NetLookupIPAddr.Name, builtinLookupIPAddr)
}
