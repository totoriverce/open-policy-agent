// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package tester

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util/test"
)

func TestRun(t *testing.T) {

	ctx := context.Background()

	files := map[string]string{
		"/a.rego": `package foo
			allow { true }
			`,
		"/a_test.rego": `package foo
			test_pass { allow }
			non_test { true }
			test_fail { not allow }
			test_fail_non_bool = 100
			test_err { conflict }
			conflict = true
			conflict = false
			`,
	}

	tests := map[[2]string]struct {
		wantErr  bool
		wantFail bool
	}{
		{"data.foo", "test_pass"}:          {false, false},
		{"data.foo", "test_fail"}:          {false, true},
		{"data.foo", "test_fail_non_bool"}: {false, true},
		{"data.foo", "test_err"}:           {true, false},
	}

	test.WithTempFS(files, func(d string) {
		rs, err := Run(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[[2]string]struct{}{}
		for i := range rs {
			k := [2]string{rs[i].Package, rs[i].Name}
			seen[k] = struct{}{}
			exp, ok := tests[k]
			if !ok {
				t.Errorf("Unexpected result for %v", k)
			} else if exp.wantErr != (rs[i].Error != nil) || exp.wantFail != (rs[i].Fail != nil) {
				t.Errorf("Expected %v for %v but got: %v", exp, k, rs[i])
			}
		}
		for k := range tests {
			if _, ok := seen[k]; !ok {
				t.Errorf("Expected result for %v", k)
			}
		}
	})
}

func TestRunnerCancel(t *testing.T) {

	ast.RegisterBuiltin(&ast.Builtin{
		Name: "test.sleep",
		Decl: types.NewFunction(
			types.Args(types.S),
			types.NewNull(),
		),
	})

	topdown.RegisterFunctionalBuiltin1("test.sleep", func(a ast.Value) (ast.Value, error) {
		d, _ := time.ParseDuration(string(a.(ast.String)))
		time.Sleep(d)
		return ast.Null{}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	module := `package foo

	test_1 { test.sleep("100ms") }
	test_2 { true }`

	files := map[string]string{
		"/a_test.rego": module,
	}

	test.WithTempFS(files, func(d string) {
		results, err := Run(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		if !topdown.IsCancel(results[0].Error) {
			t.Fatalf("Expected cancel error but got: %v", results[0].Error)
		}
	})

}

func TestTestFile(t *testing.T) {

	t.Run("unexpected undefined", func(t *testing.T) {
		files := map[string]string{
			"/foo.rego": `package ex
p = true { true }`,
		}
		test.WithTempFS(files, func(rootDir string) {
			err := TestFile(nil, "data.ex.hello", 4, nil, filepath.Join(rootDir, "foo.rego"))
			expected := fmt.Errorf("undefined")
			if err == nil {
				t.Fatalf("no error produced")
			}
			err = isExpectedError(err, expected)
			if err != nil {
				t.Fatalf(err.Error())
			}
		})
	})

	t.Run("expected undefined", func(t *testing.T) {
		files := map[string]string{
			"/foo.rego": `package ex
p = true { true }`,
		}
		test.WithTempFS(files, func(rootDir string) {
			err := TestFile(nil, "data.ex.hello", Undefined, nil, filepath.Join(rootDir, "foo.rego"))
			if err != nil {
				t.Fatalf(err.Error())
			}
		})
	})

	t.Run("expected error", func(t *testing.T) {
		files := map[string]string{
			"/foo.rego": `package test
t { x := http.send({}) }`,
		}
		test.WithTempFS(files, func(rootDir string) {
			err := TestFile(nil, "data.test.t", fmt.Errorf("operand"), nil, filepath.Join(rootDir, "foo.rego"))
			if err != nil {
				t.Fatalf(err.Error())
			}
		})
	})

	t.Run("unexpected error", func(t *testing.T) {
		files := map[string]string{
			"/foo.rego": `package test
t { x := http.send({}) }`,
		}
		test.WithTempFS(files, func(rootDir string) {
			err := TestFile(nil, "data.test.t", 12837, nil, filepath.Join(rootDir, "foo.rego"))
			expected := fmt.Errorf("operand")
			if err == nil {
				t.Fatalf("no error produced")
			}
			err = isExpectedError(err, expected)
			if err != nil {
				t.Fatalf(err.Error())
			}
		})
	})

	t.Run("inputs", func(t *testing.T) {
		files := map[string]string{
			"/foo.rego": `package test
t { input.a == input.b }`,
		}
		test.WithTempFS(files, func(rootDir string) {
			inputs := map[string]interface{}{
				"a": 1,
				"b": 1,
			}
			err := TestFile(nil, "data.test.t", true, inputs, filepath.Join(rootDir, "foo.rego"))
			if err != nil {
				t.Fatalf(err.Error())
			}
		})
	})
}
