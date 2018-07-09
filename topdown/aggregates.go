// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"math/big"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func builtinCount(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Collection:
		return ast.IntNumberTerm(a.Len()).Value, nil
	case ast.String:
		return ast.IntNumberTerm(len(a)).Value, nil
	}
	return nil, builtins.NewOperandTypeErr(1, a, "array", "object", "set", "string")
}

func builtinSum(a ast.Value) (ast.Value, error) {
	switch val := a.(type) {
	case ast.Iterable:
		sum := big.NewFloat(0)
		err := val.Iter(func(x *ast.Term) error {
			n, ok := x.Value.(ast.Number)
			if !ok {
				return builtins.NewOperandElementErr(1, a, x.Value, "number")
			}
			sum = new(big.Float).Add(sum, builtins.NumberToFloat(n))
			return nil
		})
		return builtins.FloatToNumber(sum), err
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinProduct(a ast.Value) (ast.Value, error) {
	switch val := a.(type) {
	case ast.Iterable:
		product := big.NewFloat(1)
		err := val.Iter(func(x *ast.Term) error {
			n, ok := x.Value.(ast.Number)
			if !ok {
				return builtins.NewOperandElementErr(1, a, x.Value, "number")
			}
			product = new(big.Float).Mul(product, builtins.NumberToFloat(n))
			return nil
		})
		return builtins.FloatToNumber(product), err
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinMax(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Iterable:
		if a.Len() == 0 {
			return nil, BuiltinEmpty{}
		}
		max, err := a.Reduce(ast.NullTerm(), func(max *ast.Term, elem *ast.Term) (*ast.Term, error) {
			if ast.Compare(max, elem) <= 0 {
				return elem, nil
			}
			return max, nil
		})
		return max.Value, err
	}

	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinMin(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Iterable:
		if a.Len() == 0 {
			return nil, BuiltinEmpty{}
		}
		min, err := a.Reduce(ast.NullTerm(), func(min *ast.Term, elem *ast.Term) (*ast.Term, error) {
			// The null term is considered to be less than any other term,
			// so in order for min of a set to make sense, we need to check
			// for it.
			if min.Value.Compare(ast.Null{}) == 0 {
				return elem, nil
			}

			if ast.Compare(min, elem) >= 0 {
				return elem, nil
			}
			return min, nil
		})
		return min.Value, err
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinSort(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Iterable:
		return a.Sorted(), nil
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinAll(a ast.Value) (ast.Value, error) {
	switch val := a.(type) {
	case ast.Iterable:
		res := true
		match := ast.BooleanTerm(true)
		val.Foreach(func(term *ast.Term) {
			if !term.Equal(match) {
				res = false
			}
		})
		return ast.Boolean(res), nil
	default:
		return nil, builtins.NewOperandTypeErr(1, a, "array", "set")
	}
}

func builtinAny(a ast.Value) (ast.Value, error) {
	switch val := a.(type) {
	case ast.Iterable:
		res := false
		match := ast.BooleanTerm(true)
		val.Foreach(func(term *ast.Term) {
			if term.Equal(match) {
				res = true
			}
		})
		return ast.Boolean(res), nil
	default:
		return nil, builtins.NewOperandTypeErr(1, a, "array", "set")
	}
}

func init() {
	RegisterFunctionalBuiltin1(ast.Count.Name, builtinCount)
	RegisterFunctionalBuiltin1(ast.Sum.Name, builtinSum)
	RegisterFunctionalBuiltin1(ast.Product.Name, builtinProduct)
	RegisterFunctionalBuiltin1(ast.Max.Name, builtinMax)
	RegisterFunctionalBuiltin1(ast.Min.Name, builtinMin)
	RegisterFunctionalBuiltin1(ast.Sort.Name, builtinSort)
	RegisterFunctionalBuiltin1(ast.All.Name, builtinAll)
	RegisterFunctionalBuiltin1(ast.Any.Name, builtinAny)
}
