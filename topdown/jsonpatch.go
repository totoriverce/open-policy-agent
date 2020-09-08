// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"encoding/json"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func builtinJSONPatchApply(_ BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	// Expect an arr/set of JSON Patch operations and a target arr/object
	ops, err := getPatchOps(operands[0].Value)
	if err != nil {
		return err
	}

	target, err := getTargetDoc(operands[1].Value)
	if err != nil {
		return err
	}

	patch, err := jsonpatch.DecodePatch(ops)
	if err != nil {
		return err
	}

	result, err := patch.Apply(target)
	if err != nil {
		// handle invalid "op" error more clearly
		if strings.HasPrefix(err.Error(), "Unexpected kind: ") {
			return builtins.NewOperandErr(1, "must contain valid JSON Patch operations")
		}
		return err
	}

	var resultInterface interface{}
	if err := json.Unmarshal(result, &resultInterface); err != nil {
		return err
	}

	resultVal, err := ast.InterfaceToValue(resultInterface)
	if err != nil {
		return err
	}

	return iter(ast.NewTerm(resultVal))
}

func getPatchOps(arrayOrSet ast.Value) ([]byte, error) {
	var out interface{}
	var err error

	switch v := arrayOrSet.(type) {
	case *ast.Array, ast.Set:
		out, err = ast.JSON(v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, builtins.NewOperandTypeErr(1, arrayOrSet, "set", "array")
	}

	return json.Marshal(out)
}

func getTargetDoc(arrayOrObj ast.Value) ([]byte, error) {
	var out interface{}
	var err error

	switch v := arrayOrObj.(type) {
	case *ast.Array, ast.Object:
		out, err = ast.JSON(v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, builtins.NewOperandTypeErr(1, arrayOrObj, "array", "object")
	}

	return json.Marshal(out)
}

func init() {
	RegisterBuiltinFunc(ast.JSONPatchApply.Name, builtinJSONPatchApply)
}
