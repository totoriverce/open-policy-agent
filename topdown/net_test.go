package topdown

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/foxcpp/go-mockdns"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func TestNetLookupIPAddr(t *testing.T) {
	srv, err := mockdns.NewServerWithLogger(map[string]mockdns.Zone{
		"v4.org.": {
			A: []string{"1.2.3.4"},
		},
		"v6.org.": {
			AAAA: []string{"1:2:3::4"},
		},
		"v4-v6.org.": {
			A:    []string{"1.2.3.4"},
			AAAA: []string{"1:2:3::4"},
		},
		"error.org.": {
			Err: fmt.Errorf("OH NO"),
		},
	}, sink{}, true)
	if err != nil {
		t.Fatal(err)
	}

	srvFail, err := mockdns.NewServerWithLogger(map[string]mockdns.Zone{}, sink{}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srvFail.Close() })
	t.Cleanup(func() { mutateResolver(mockdns.UnpatchNet) })

	for addr, exp := range map[string]ast.Set{
		"v4.org":    ast.NewSet(ast.StringTerm("1.2.3.4")),
		"v6.org":    ast.NewSet(ast.StringTerm("1:2:3::4")),
		"v4-v6.org": ast.NewSet(ast.StringTerm("1.2.3.4"), ast.StringTerm("1:2:3::4")),
	} {
		t.Run(addr, func(t *testing.T) {
			bctx := BuiltinContext{
				Context: context.Background(),
				Cache:   make(builtins.Cache),
			}
			mutateResolver(srv.PatchNet)
			err := builtinLookupIPAddr(bctx, []*ast.Term{ast.StringTerm(addr)}, func(act *ast.Term) error {
				if exp.Compare(act.Value) != 0 {
					t.Errorf("expected %v, got %v", exp, act)
				}
				return nil
			})
			if err != nil {
				t.Error(err)
			}

			// check cache put
			act, ok := bctx.Cache.Get(lookupIPAddrCacheKey(addr))
			if !ok {
				t.Fatal("result not put into cache")
			}
			if exp.Compare(act.(*ast.Term).Value) != 0 {
				t.Errorf("cache: expected %v, got %v", exp, act)
			}

			// exercise cache hit
			mutateResolver(srvFail.PatchNet)
			err = builtinLookupIPAddr(bctx, []*ast.Term{ast.StringTerm(addr)}, func(act *ast.Term) error {
				if exp.Compare(act.Value) != 0 {
					t.Errorf("expected %v, got %v", exp, act)
				}
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}

	for _, addr := range []string{"error.org", "nosuch.org"} {
		t.Run(addr, func(t *testing.T) {
			bctx := BuiltinContext{
				Context: context.Background(),
				Cache:   make(builtins.Cache),
			}
			mutateResolver(srv.PatchNet)
			err := builtinLookupIPAddr(bctx, []*ast.Term{ast.StringTerm(addr)}, func(*ast.Term) error {
				t.Fatal("expected not to be called")
				return nil
			})
			if err == nil {
				t.Error("expected error")
			}
			if testing.Verbose() {
				t.Log(err)
			}
		})
	}

	cancelled := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	timedOut := func() context.Context {
		ctx, _ := context.WithTimeout(context.Background(), time.Nanosecond) // nolint
		return ctx
	}

	for name, ctx := range map[string]func() context.Context{
		"cancelled": cancelled,
		"timed out": timedOut,
	} {
		t.Run(name, func(t *testing.T) {
			bctx := BuiltinContext{
				Context: ctx(),
				Cache:   make(builtins.Cache),
			}
			mutateResolver(srv.PatchNet)
			err := builtinLookupIPAddr(bctx, []*ast.Term{ast.StringTerm("example.org")}, func(*ast.Term) error {
				t.Fatal("expected not to be called")
				return nil
			})
			if err == nil {
				t.Fatal("expected error")
			}
			_, ok := err.(Halt)
			if !ok {
				t.Errorf("expected Halt error, got %v (%[1]T)", err)
			}
			if !IsCancel(err) {
				t.Errorf("expected wrapped Cancel error, got %v (%[1]T)", err)
			}
		})
	}
}

type sink struct{}

func (sink) Printf(string, ...interface{}) {}
