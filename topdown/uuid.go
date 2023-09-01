// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/internal/uuid"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

type uuidCachingKey string

func builtinUUIDRFC4122(bctx BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {

	var key = uuidCachingKey(operands[0].Value.String())

	val, ok := bctx.Cache.Get(key)
	if ok {
		return iter(val.(*ast.Term))
	}

	s, err := uuid.New(bctx.Seed)
	if err != nil {
		return err
	}

	result := ast.NewTerm(ast.String(s))
	bctx.Cache.Put(key, result)

	return iter(result)
}

func builtinUUIDParse(_ BuiltinContext, operands []*ast.Term, iter func(term *ast.Term) error) error {
	str, err := builtins.StringOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	parsed, err := uuid.Parse(string(str))
	if err != nil {
		return nil
	}

	result := ast.NewObject()
	for prop, val := range parsed {
		switch val := val.(type) {
		case string:
			result.Insert(ast.StringTerm(prop), ast.StringTerm(val))
		case int:
			result.Insert(ast.StringTerm(prop), ast.IntNumberTerm(val))
		case int64:
			result.Insert(ast.StringTerm(prop), ast.NumberTerm(int64ToJSONNumber(val)))
		}
	}
	return iter(ast.NewTerm(result))
}

func init() {
	RegisterBuiltinFunc(ast.UUIDRFC4122.Name, builtinUUIDRFC4122)
	RegisterBuiltinFunc(ast.UUIDParse.Name, builtinUUIDParse)
}
