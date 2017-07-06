// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/util"
	"github.com/open-policy-agent/opa/util/test"
)

func TestModuleTree(t *testing.T) {

	mods := getCompilerTestModules()
	mods["system-mod"] = MustParseModule(`
	package system.foo

	p = 1
	`)
	mods["non-system-mod"] = MustParseModule(`
	package user.system

	p = 1
	`)
	tree := NewModuleTree(mods)
	expectedSize := 9

	if tree.Size() != expectedSize {
		t.Fatalf("Expected %v but got %v modules", expectedSize, tree.Size())
	}

	if !tree.Children[Var("data")].Children[String("system")].Hide {
		t.Fatalf("Expected system node to be hidden")
	}

	if tree.Children[Var("data")].Children[String("system")].Children[String("foo")].Hide {
		t.Fatalf("Expected system.foo node to be visible")
	}

	if tree.Children[Var("data")].Children[String("user")].Children[String("system")].Hide {
		t.Fatalf("Expected user.system node to be visible")
	}

}

func TestRuleTree(t *testing.T) {

	mods := getCompilerTestModules()
	mods["system-mod"] = MustParseModule(`
	package system.foo

	p = 1
	`)
	mods["non-system-mod"] = MustParseModule(`
	package user.system

	p = 1
	`)
	mods["mod-incr"] = MustParseModule(`package a.b.c

s[1] { true }
s[2] { true }`,
	)

	tree := NewRuleTree(NewModuleTree(mods))
	expectedNumRules := 21

	if tree.Size() != expectedNumRules {
		t.Errorf("Expected %v but got %v rules", expectedNumRules, tree.Size())
	}

	// Check that empty packages are represented as leaves with no rules.
	node := tree.Children[Var("data")].Children[String("a")].Children[String("b")].Children[String("empty")]

	if node == nil || len(node.Children) != 0 || len(node.Values) != 0 {
		t.Fatalf("Unexpected nil value or non-empty leaf of non-leaf node: %v", node)
	}

	system := tree.Child(Var("data")).Child(String("system"))
	if !system.Hide {
		t.Fatalf("Expected system node to be hidden")
	}

	if system.Child(String("foo")).Hide {
		t.Fatalf("Expected system.foo node to be visible")
	}

	user := tree.Child(Var("data")).Child(String("user")).Child(String("system"))
	if user.Hide {
		t.Fatalf("Expected user.system node to be visible")
	}
}

func TestCompilerEmpty(t *testing.T) {
	c := NewCompiler()
	c.Compile(nil)
	assertNotFailed(t, c)
}

func TestCompilerExample(t *testing.T) {
	c := NewCompiler()
	m := MustParseModule(testModule)
	c.Compile(map[string]*Module{"testMod": m})
	assertNotFailed(t, c)
}

func TestCompilerUserFunction(t *testing.T) {
	tests := []struct {
		modules []string
		assert  func(*testing.T, *Compiler)
		errs    []string
	}{
		{
			// Test that functions can have different input types.
			modules: []string{`package x

				f([x]) = y {
					y = x
				}

				f({"foo": x}) = y {
					y = x
				}`},
			assert: assertNotFailed,
		},
		{
			modules: []string{`package x

				f([x]) = y {
					y = x
				}

				f([[x]]) = y {
					y = x
				}`},
			assert: assertNotFailed,
		},
		{
			modules: []string{`package x

				f(1, x) = y {
					y = x
				}

				f(x, y) = z {
					z = x+y
				}`},
			assert: assertNotFailed,
		},
		{
			modules: []string{`package x

				f(x, 1) = y {
					y = x
				}

				f(x, [y]) = z {
					z = x+y
				}`},
			assert: assertNotFailed,
		},
		{
			modules: []string{`package x

				f({"foo": {"bar": x}}) = y {
					y = x
				}

				f({"foo": [x]}) = y {
					y = x
				}`},
			assert: assertNotFailed,
		},
		{
			modules: []string{`package x

				f(1) = y {
					y = "foo"
				}

				f(2) = y {
					y = 2
				}`},
			assert: assertNotFailed,
		},
		{
			modules: []string{`package x

				f(1) = y {
					y = "foo"
				}

				f(2) = y {
					y = "bar"
				}`},
			assert: assertNotFailed,
		},
		// Test that functions must have the same number of inputs.
		{
			modules: []string{`package x

				f(x) = y {
					y = x
				}

				f(x, y) = z {
					z = x+y
				}

				f(x, y, z) = a {
					b = x+y
					a = b+z
				}`},
			assert: assertFailed,
		},
		// Test that a function and rule within the same package cannot
		// share a name.
		{
			modules: []string{`package x

			f(x) = y {
				y = x
			}

			f[x] = y {
				x = "foo"
				y = "bar"
			}`},
			assert: assertFailed,
		},
		// Test that a function and rule within different packages can
		// share a name.
		{
			modules: []string{
				`package x

				f(x) = y {
					data.y.f[x] = y
				}`,
				`package y

				f[x] = y {
					y = "bar"
					x = "foo"
				}`,
			},
			assert: assertNotFailed,
		},
		// Test that a reference to a function that does not invoke it
		// errors.
		{
			modules: []string{
				`package x

				f(x) = y {
					y = x
				}

				g = y {
					y = "foo"
					f
				}`,
				`package y

				g = y {
					y = "foo"
					x.f
				}`,
			},
			assert: assertFailed,
			errs: []string{
				"rego_compile_error: mod0:9: f refers to a known builtin but does not call it",
				"rego_compile_error: mod1:5: x.f refers to a known builtin but does not call it",
			},
		},
		// Test that void functions compile properly.
		{
			modules: []string{
				`package x

				f(x) {
					x = "foo"
				}`},
			assert: assertNotFailed,
		},
		// Test that unsafe outputs do not result in panics when checking
		// refs, instead they should just fail to compile.
		{
			modules: []string{`package x

				f(x) = y {
					noexist(x, b)
					y = b[0]
				}`},
			assert: assertFailed,
		},
	}
	for _, test := range tests {
		var err error
		modules := map[string]*Module{}
		for i, module := range test.modules {
			name := fmt.Sprintf("mod%d", i)
			modules[name], err = ParseModule(name, module)
			if err != nil {
				panic(err)
			}
		}

		c := NewCompiler()
		c.Compile(modules)
		test.assert(t, c)

		if test.errs != nil {
			errs := c.Errors
			var result []string
			for _, err := range errs {
				result = append(result, err.Error())
			}

			sort.Strings(test.errs)
			sort.Strings(result)
			if !reflect.DeepEqual(test.errs, result) {
				t.Errorf("Expected errors %v test.errs, got %v", test.errs, result)
			}
		}
	}
}

func TestCompilerCheckSafetyHead(t *testing.T) {
	c := NewCompiler()
	c.Modules = getCompilerTestModules()
	c.Modules["newMod"] = MustParseModule(`package a.b

unboundKey[x] = y { q[y] = {"foo": [1, 2, [{"bar": y}]]} }
unboundVal[y] = x { q[y] = {"foo": [1, 2, [{"bar": y}]]} }
unboundCompositeVal[y] = [{"foo": x, "bar": y}] { q[y] = {"foo": [1, 2, [{"bar": y}]]} }
unboundCompositeKey[[{"x": x}]] { q[y] }
unboundBuiltinOperator = eq { x = 1 }
unboundElse { false } else = else_var { true }
`,
	)
	compileStages(c, c.checkSafetyRuleHeads)

	makeErrMsg := func(v string) string {
		return fmt.Sprintf("rego_unsafe_var_error: var %v is unsafe", v)
	}

	expected := []string{
		makeErrMsg("x"),
		makeErrMsg("x"),
		makeErrMsg("x"),
		makeErrMsg("x"),
		makeErrMsg("eq"),
		makeErrMsg("else_var"),
	}

	result := compilerErrsToStringSlice(c.Errors)
	sort.Strings(expected)

	if len(result) != len(expected) {
		t.Fatalf("Expected %d:\n%v\nBut got %d:\n%v", len(expected), strings.Join(expected, "\n"), len(result), strings.Join(result, "\n"))
	}

	for i := range result {
		if expected[i] != result[i] {
			t.Errorf("Expected %v but got: %v", expected[i], result[i])
		}
	}

}

func TestCompilerCheckSafetyBodyReordering(t *testing.T) {
	tests := []struct {
		note     string
		body     string
		expected string
	}{
		{"noop", `x = 1; x != 0`, `x = 1; x != 0`},
		{"var/ref", `a[i] = x; a = [1, 2, 3, 4]`, `a = [1, 2, 3, 4]; a[i] = x`},
		{"var/ref (nested)", `a = [1, 2, 3, 4]; a[b[i]] = x; b = [0, 0, 0, 0]`, `a = [1, 2, 3, 4]; b = [0, 0, 0, 0]; a[b[i]] = x`},
		{"negation",
			`a = [true, false]; b = [true, false]; not a[i]; b[i]`,
			`a = [true, false]; b = [true, false]; b[i]; not a[i]`},
		{"built-in", `x != 0; count([1, 2, 3], x)`, `count([1, 2, 3], x); x != 0`},
		{"var/var 1", `x = y; z = 1; y = z`, `z = 1; y = z; x = y`},
		{"var/var 2", `x = y; 1 = z; z = y`, `1 = z; z = y; x = y`},
		{"var/var 3", `x != 0; y = x; y = 1`, `y = 1; y = x; x != 0`},
		{"array compr/var", `x != 0; [y | y = 1] = x`, `[y | y = 1] = x; x != 0`},
		{"array compr/array", `[1] != [x]; [y | y = 1] = [x]`, `[y | y = 1] = [x]; [1] != [x]`},
		{"with", `data.a.b.d.t with input as x; x = 1`, `x = 1; data.a.b.d.t with input as x`},
		{"with-2", `data.a.b.d.t with input.x as x; x = 1`, `x = 1; data.a.b.d.t with input.x as x`},
		{"with-nop", "data.somedoc[x] with input as true", "data.somedoc[x] with input as true"},
		{"ref-head", `s = [["foo"], ["bar"]]; x = y[0]; y = s[_]; contains(x, "oo")`, `
			s = [["foo"], ["bar"]];
			y = s[_];
			x = y[0];
			contains(x, "oo")
		`},
		{"userfunc", `split(y, ".", z); a.b.funcs.fn("...foo.bar..", y)`, `a.b.funcs.fn("...foo.bar..", y); split(y, ".", z)`},
	}

	for i, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			c := NewCompiler()
			c.Modules = getCompilerTestModules()
			c.Modules["reordering"] = MustParseModule(fmt.Sprintf(
				`package test
			 p { %s }`, tc.body))

			compileStages(c, c.checkSafetyRuleBodies)

			if c.Failed() {
				t.Errorf("%v (#%d): Unexpected compilation error: %v", tc.note, i, c.Errors)
				return
			}

			expected := MustParseBody(tc.expected)
			result := c.Modules["reordering"].Rules[0].Body

			if !expected.Equal(result) {
				t.Errorf("%v (#%d): Expected body to be ordered and equal to %v but got: %v", tc.note, i, expected, result)
			}
		})
	}
}

func TestCompilerCheckSafetyBodyReorderingClosures(t *testing.T) {
	c := NewCompiler()
	c.Modules = map[string]*Module{
		"mod": MustParseModule(
			`package compr

import data.b
import data.c

fn(x) = y {
	trim(x, ".", y)
}

p = true { v = [null | true]; xs = [x | a[i] = x; a = [y | y != 1; y = c[j]]]; xs[j] > 0; z = [true | data.a.b.d.t with input as i2; i2 = i]; b[i] = j }
q = true { _ = [x | x = b[i]]; _ = b[j]; _ = [x | x = true; x != false]; true != false; _ = [x | data.foo[_] = x]; data.foo[_] = _ }
r = true { a = [x | split(y, ".", z); x = z[i]; fn("...foo.bar..", y)] }`,
		),
	}

	compileStages(c, c.checkSafetyRuleBodies)
	assertNotFailed(t, c)

	result1 := c.Modules["mod"].Rules[0].Body
	expected1 := MustParseBody(`v = [null | true]; data.b[i] = j; xs = [x | a = [y | y = data.c[j]; y != 1]; a[i] = x]; z = [true | i2 = i; data.a.b.d.t with input as i2]; xs[j] > 0`)
	if !result1.Equal(expected1) {
		t.Errorf("Expected reordered body to be equal to:\n%v\nBut got:\n%v", expected1, result1)
	}

	result2 := c.Modules["mod"].Rules[1].Body
	expected2 := MustParseBody(`_ = [x | x = data.b[i]]; _ = data.b[j]; _ = [x | x = true; x != false]; true != false; _ = [x | data.foo[_] = x]; data.foo[_] = _`)
	if !result2.Equal(expected2) {
		t.Errorf("Expected pre-ordered body to equal:\n%v\nBut got:\n%v", expected2, result2)
	}

	result3 := c.Modules["mod"].Rules[2].Body
	expected3 := MustParseBody(`a = [x | compr.fn("...foo.bar..", y); split(y, ".", z); x = z[i]]`)
	if !result3.Equal(expected3) {
		t.Errorf("Expected pre-ordered body to equal:\n%v\nBut got:\n%v", expected3, result3)
	}
}

func TestCompilerCheckSafetyBodyErrors(t *testing.T) {

	moduleBegin := `
		package a.b

		import input.aref.b.c as foo
		import input.avar as bar
		import data.m.n as baz
	`

	tests := []struct {
		note          string
		moduleContent string
		expected      string
	}{
		{"ref-head", `p { a.b.c = "foo" }`, `{a,}`},
		{"ref-head-2", `p { {"foo": [{"bar": a.b.c}]} = {"foo": [{"bar": "baz"}]} }`, `{a,}`},
		{"negation", `p { a = [1, 2, 3, 4]; not a[i] = x }`, `{i, x}`},
		{"negation-head", `p[x] { a = [1, 2, 3, 4]; not a[i] = x }`, `{i,x}`},
		{"negation-multiple", `p { a = [1, 2, 3, 4]; b = [1, 2, 3, 4]; not a[i] = x; not b[j] = x }`, `{i, x, j}`},
		{"negation-nested", `p { a = [{"foo": ["bar", "baz"]}]; not a[0].foo = [a[0].foo[i], a[0].foo[j]] } `, `{i, j}`},
		{"builtin-input", `p { count([1, 2, x], x) }`, `{x,}`},
		{"builtin-input-name", `p { count(eq, 1) }`, `{eq,}`},
		{"builtin-multiple", `p { x > 0; x <= 3; x != 2 }`, `{x,}`},
		{"array-compr", `p { _ = [x | x = data.a[_]; y > 1] }`, `{y,}`},
		{"array-compr-nested", `p { _ = [x | x = a[_]; a = [y | y = data.a[_]; z > 1]] }`, `{z,}`},
		{"array-compr-closure", `p { _ = [v | v = [x | x = data.a[_]]; x > 1] }`, `{x,}`},
		{"array-compr-term", `p { _ = [u | true] }`, `{u,}`},
		{"array-compr-term-nested", `p { _ = [v | v = [w | w != 0]] }`, `{w,}`},
		{"array-compr-term-output", `p { _ = [x[i] | x = []] }`, `{i,}`},
		{"array-compr-mixed", `p { _ = [x | y = [a | a = z[i]]] }`, `{a, x, z, i}`},
		{"array-compr-builtin", `p { [true | eq != 2] }`, `{eq,}`},
		{"closure-self", `p { x = [x | x = 1] }`, `{x,}`},
		{"closure-transitive", `p { x = y; x = [y | y = 1] }`, `{y,}`},
		{"nested", `p { count(baz[i].attr[bar[dead.beef]], n) }`, `{dead,}`},
		{"negated-import", `p { not foo; not bar; not baz }`, `set()`},
		{"rewritten", `p[{"foo": dead[i]}] { true }`, `{dead, i}`},
		{"with-value", `p { data.a.b.d.t with input as x }`, `{x,}`},
		{"with-value-2", `p { x = data.a.b.d.t with input as x }`, `{x,}`},
		{"else-kw", "p { false } else { count(x, 1) }", `{x,}`},
		{"userfunc", "foo(x) = [y, z] { split(x, y, z) }", `{y,z}`},
	}

	makeErrMsg := func(varName string) string {
		return fmt.Sprintf("rego_unsafe_var_error: var %v is unsafe", varName)
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {

			// Build slice of expected error messages.
			expected := []string{}

			MustParseTerm(tc.expected).Value.(*Set).Iter(func(x *Term) bool {
				expected = append(expected, makeErrMsg(string(x.Value.(Var))))
				return false
			})

			sort.Strings(expected)

			// Compile test module.
			c := NewCompiler()
			c.Modules = map[string]*Module{
				"newMod": MustParseModule(fmt.Sprintf(`

				%v

				%v

				`, moduleBegin, tc.moduleContent)),
			}

			compileStages(c, c.checkSafetyRuleBodies)

			// Get errors.
			result := compilerErrsToStringSlice(c.Errors)

			// Check against expected.
			if len(result) != len(expected) {
				t.Fatalf("Expected %d:\n%v\nBut got %d:\n%v", len(expected), strings.Join(expected, "\n"), len(result), strings.Join(result, "\n"))
			}

			for i := range result {
				if expected[i] != result[i] {
					t.Errorf("Expected %v but got: %v", expected[i], result[i])
				}
			}

		})
	}
}

func TestCompilerCheckWithModifiers(t *testing.T) {

	c := NewCompiler()
	c.Modules = getCompilerTestModules()
	c.Modules["with-modifiers"] = MustParseModule(`package badwith

import data.a.b.d.t as req_dep

p = true { true }
ref_in_value = true { req_dep with input as p }
closure_in_value = true { req_dep with input as [null | null] }
data_target = true { req_dep with data.p as "foo" }`,
	)

	compileStages(c, c.checkWithModifiers)

	expected := []string{
		"rego_type_error: closure_in_value: with keyword value must not contain closures",
		"rego_type_error: data_target: with keyword target must be input",
		"rego_type_error: ref_in_value: with keyword value must not contain refs",
	}

	assertCompilerErrorStrings(t, c, expected)

}

func TestCompilerCheckTypes(t *testing.T) {
	c := NewCompiler()
	modules := getCompilerTestModules()
	c.Modules = map[string]*Module{"mod6": modules["mod6"], "mod7": modules["mod7"]}
	compileStages(c, c.checkTypes)
	assertNotFailed(t, c)
}

func TestCompilerCheckRuleConflicts(t *testing.T) {

	c := getCompilerWithParsedModules(map[string]string{
		"mod1.rego": `package badrules

p[x] { x = 1 }
p[x] = y { x = y; x = "a" }
q[1] { true }
q = {1, 2, 3} { true }
r[x] = y { x = y; x = "a" }
r[x] = y { x = y; x = "a" }`,

		"mod2.rego": `package badrules.r

q[1] { true }`,

		"mod3.rego": `package badrules.defkw

default foo = 1
default foo = 2
foo = 3 { true }`,
	})

	compileStages(c, c.checkRuleConflicts)

	expected := []string{
		"rego_type_error: conflicting rules named p found",
		"rego_type_error: conflicting rules named q found",
		"rego_type_error: multiple default rules named foo found",
		"rego_type_error: package badrules.r conflicts with rule defined at mod1.rego:7",
		"rego_type_error: package badrules.r conflicts with rule defined at mod1.rego:8",
	}

	assertCompilerErrorStrings(t, c, expected)
}

func TestCompilerImportsResolved(t *testing.T) {

	modules := map[string]*Module{
		"mod1": MustParseModule(`package ex

import data
import input
import data.foo
import input.bar
import data.abc as baz
import input.abc as qux`,
		),
	}

	c := NewCompiler()
	c.Compile(modules)

	assertNotFailed(t, c)

	if len(c.Modules["mod1"].Imports) != 0 {
		t.Fatalf("Expected imports to be empty after compile but got: %v", c.Modules)
	}

}

func TestCompilerResolveAllRefs(t *testing.T) {
	c := NewCompiler()
	c.Modules = getCompilerTestModules()
	c.Modules["head"] = MustParseModule(`package head

import data.doc1 as bar
import input.x.y.foo
import input.qux as baz

p[foo[bar[i]]] = {"baz": baz} { true }`)

	c.Modules["elsekw"] = MustParseModule(`package elsekw

	import input.x.y.foo
	import data.doc1 as bar
	import input.baz

	p {
		false
	} else = foo {
		bar
	} else = baz {
		true
	}
	`)

	compileStages(c, c.resolveAllRefs)
	assertNotFailed(t, c)

	// Basic test cases.
	mod1 := c.Modules["mod1"]
	p := mod1.Rules[0]
	expr1 := p.Body[0]
	term := expr1.Terms.(*Term)
	e := MustParseTerm("data.a.b.c.q[x]")
	if !term.Equal(e) {
		t.Errorf("Wrong term (global in same module): expected %v but got: %v", e, term)
	}

	expr2 := p.Body[1]
	term = expr2.Terms.(*Term)
	e = MustParseTerm("data.a.b.c.r[x]")
	if !term.Equal(e) {
		t.Errorf("Wrong term (global in same package/diff module): expected %v but got: %v", e, term)
	}

	mod2 := c.Modules["mod2"]
	r := mod2.Rules[0]
	expr3 := r.Body[1]
	term = expr3.Terms.([]*Term)[1]
	e = MustParseTerm("data.x.y.p")
	if !term.Equal(e) {
		t.Errorf("Wrong term (var import): expected %v but got: %v", e, term)
	}

	mod3 := c.Modules["mod3"]
	expr4 := mod3.Rules[0].Body[0]
	term = expr4.Terms.([]*Term)[2]
	e = MustParseTerm("{input.x.secret: [{input.x.keyid}]}")
	if !term.Equal(e) {
		t.Errorf("Wrong term (nested refs): expected %v but got: %v", e, term)
	}

	// Array comprehensions.
	mod5 := c.Modules["mod5"]

	ac := func(r *Rule) *ArrayComprehension {
		return r.Body[0].Terms.(*Term).Value.(*ArrayComprehension)
	}

	acTerm1 := ac(mod5.Rules[0])
	assertTermEqual(t, acTerm1.Term, MustParseTerm("input.x.a"))
	acTerm2 := ac(mod5.Rules[1])
	assertTermEqual(t, acTerm2.Term, MustParseTerm("data.a.b.c.q.a"))
	acTerm3 := ac(mod5.Rules[2])
	assertTermEqual(t, acTerm3.Body[0].Terms.([]*Term)[1], MustParseTerm("input.x.a"))
	acTerm4 := ac(mod5.Rules[3])
	assertTermEqual(t, acTerm4.Body[0].Terms.([]*Term)[1], MustParseTerm("data.a.b.c.q[i]"))
	acTerm5 := ac(mod5.Rules[4])
	assertTermEqual(t, acTerm5.Body[0].Terms.([]*Term)[2].Value.(*ArrayComprehension).Term, MustParseTerm("input.x.a"))
	acTerm6 := ac(mod5.Rules[5])
	assertTermEqual(t, acTerm6.Body[0].Terms.([]*Term)[2].Value.(*ArrayComprehension).Body[0].Terms.([]*Term)[1], MustParseTerm("data.a.b.c.q[i]"))

	// Nested references.
	mod6 := c.Modules["mod6"]
	nested1 := mod6.Rules[0].Body[0].Terms.(*Term)
	assertTermEqual(t, nested1, MustParseTerm("data.x[input.x[i].a[data.z.b[j]]]"))

	nested2 := mod6.Rules[1].Body[1].Terms.(*Term)
	assertTermEqual(t, nested2, MustParseTerm("v[input.x[i]]"))

	nested3 := mod6.Rules[3].Body[0].Terms.(*Term)
	assertTermEqual(t, nested3, MustParseTerm("data.x[data.a.b.nested.r]"))

	// Refs in head.
	mod7 := c.Modules["head"]
	assertTermEqual(t, mod7.Rules[0].Head.Key, MustParseTerm("input.x.y.foo[data.doc1[i]]"))
	assertTermEqual(t, mod7.Rules[0].Head.Value, MustParseTerm(`{"baz": input.qux}`))

	// Refs in else.
	mod8 := c.Modules["elsekw"]
	assertTermEqual(t, mod8.Rules[0].Else.Head.Value, MustParseTerm("input.x.y.foo"))
	assertTermEqual(t, mod8.Rules[0].Else.Body[0].Terms.(*Term), MustParseTerm("data.doc1"))
	assertTermEqual(t, mod8.Rules[0].Else.Else.Head.Value, MustParseTerm("input.baz"))

	// Locations.
	parsedLoc := getCompilerTestModules()["mod1"].Rules[0].Body[0].Terms.(*Term).Value.(Ref)[0].Location
	compiledLoc := c.Modules["mod1"].Rules[0].Body[0].Terms.(*Term).Value.(Ref)[0].Location
	if parsedLoc.Row != compiledLoc.Row {
		t.Fatalf("Expected parsed location (%v) and compiled location (%v) to be equal", parsedLoc.Row, compiledLoc.Row)
	}
}

func TestCompilerRewriteTermsInHead(t *testing.T) {
	c := NewCompiler()
	c.Modules["head"] = MustParseModule(`package head

import data.doc1 as bar
import data.doc2 as corge
import input.x.y.foo
import input.qux as baz

p[foo[bar[i]]] = {"baz": baz, "corge": corge} { true }
q = [true | true] { true }
r = {"true": true | true} { true }
s = {true | true} { true }

elsekw {
	false
} else = baz {
	true
}
`)

	compileStages(c, c.rewriteRefsInHead)
	assertNotFailed(t, c)

	rule1 := c.Modules["head"].Rules[0]
	expected1 := MustParseRule(`p[__local0__] = __local1__ { true; __local0__ = input.x.y.foo[data.doc1[i]]; __local1__ = {"baz": input.qux, "corge": data.doc2} }`)
	assertRulesEqual(t, rule1, expected1)

	rule2 := c.Modules["head"].Rules[1]
	expected2 := MustParseRule(`q = __local2__ { true; __local2__ = [true | true] }`)
	assertRulesEqual(t, rule2, expected2)

	rule3 := c.Modules["head"].Rules[2]
	expected3 := MustParseRule(`r = __local3__ { true; __local3__ = {"true": true | true} }`)
	assertRulesEqual(t, rule3, expected3)

	rule4 := c.Modules["head"].Rules[3]
	expected4 := MustParseRule(`s = __local4__ { true; __local4__ = {true | true} }`)
	assertRulesEqual(t, rule4, expected4)

	rule5 := c.Modules["head"].Rules[4]
	expected5 := MustParseRule(`elsekw { false } else = __local5__ { true; __local5__ = input.qux }`)
	assertRulesEqual(t, rule5, expected5)
}

func TestCompilerSetGraph(t *testing.T) {
	c := NewCompiler()
	c.Modules = getCompilerTestModules()
	c.Modules["elsekw"] = MustParseModule(`
	package elsekw

	p {
		false
	} else = q {
		false
	} else {
		r
	}

	q = true
	r = true
	`)
	compileStages(c, c.setGraph)

	assertNotFailed(t, c)

	mod1 := c.Modules["mod1"]
	p := mod1.Rules[0]
	q := mod1.Rules[1]
	mod2 := c.Modules["mod2"]
	r := mod2.Rules[0]

	edges := map[util.T]struct{}{
		q: struct{}{},
		r: struct{}{},
	}

	if !reflect.DeepEqual(edges, c.Graph.Dependencies(p)) {
		t.Fatalf("Expected dependencies for p to be q and r but got: %v", c.Graph.Dependencies(p))
	}

	sorted, ok := c.Graph.Sort()
	if !ok {
		t.Fatalf("Expected sort to succeed.")
	}

	numRules := 0
	numFuncs := 0

	for _, module := range c.Modules {
		WalkRules(module, func(*Rule) bool {
			numRules++
			return false
		})
		WalkFuncs(module, func(*Func) bool {
			numFuncs++
			return false
		})
	}

	if len(sorted) != numRules+numFuncs {
		t.Fatalf("Expected numRules+numFuncs (%v) to be same as len(sorted) (%v)", numRules+numFuncs, len(sorted))
	}

	// Probe rules with dependencies. Ordering is not stable for ties because
	// nodes are stored in a map.
	probes := [][2]*Rule{
		{c.Modules["mod1"].Rules[1], c.Modules["mod1"].Rules[0]},               // mod1.q before mod1.p
		{c.Modules["mod2"].Rules[0], c.Modules["mod1"].Rules[0]},               // mod2.r before mod1.p
		{c.Modules["mod1"].Rules[1], c.Modules["mod5"].Rules[1]},               // mod1.q before mod5.r
		{c.Modules["mod1"].Rules[1], c.Modules["mod5"].Rules[3]},               // mod1.q before mod6.t
		{c.Modules["mod1"].Rules[1], c.Modules["mod5"].Rules[5]},               // mod1.q before mod6.v
		{c.Modules["mod6"].Rules[2], c.Modules["mod6"].Rules[3]},               // mod6.r before mod6.s
		{c.Modules["elsekw"].Rules[1], c.Modules["elsekw"].Rules[0].Else},      // elsekw.q before elsekw.p.else
		{c.Modules["elsekw"].Rules[2], c.Modules["elsekw"].Rules[0].Else.Else}, // elsekw.r before elsekw.p.else.else
	}

	getSortedIdx := func(r *Rule) int {
		for i := range sorted {
			if sorted[i] == r {
				return i
			}
		}
		return -1
	}

	for num, probe := range probes {
		i := getSortedIdx(probe[0])
		j := getSortedIdx(probe[1])
		if i == -1 || j == -1 {
			t.Fatalf("Expected to find probe %d in sorted slice but got: i=%d, j=%d", num+1, i, j)
		}
		if i >= j {
			t.Errorf("Sort order of probe %d (A) %v and (B) %v and is wrong (expected A before B)", num+1, probe[0], probe[1])
		}
	}
}

func TestGraphCycle(t *testing.T) {
	mod1 := `package a.b.c

	p { q }
	q { r }
	r { s }
	s { q }`

	c := NewCompiler()
	c.Modules = map[string]*Module{
		"mod1": MustParseModule(mod1),
	}

	compileStages(c, c.setGraph)
	assertNotFailed(t, c)

	_, ok := c.Graph.Sort()
	if ok {
		t.Fatalf("Expected to find cycle in rule graph")
	}

	elsekw := `package elsekw

	p {
		false
	} else = q {
		true
	}

	q {
		false
	} else {
		r
	}

	r { s }

	s { p }
	`

	c = NewCompiler()
	c.Modules = map[string]*Module{
		"elsekw": MustParseModule(elsekw),
	}

	compileStages(c, c.setGraph)
	assertNotFailed(t, c)

	_, ok = c.Graph.Sort()
	if ok {
		t.Fatalf("Expected to find cycle in rule graph")
	}

}

func TestCompilerCheckRecursion(t *testing.T) {
	c := NewCompiler()
	c.Modules = map[string]*Module{
		"newMod1": MustParseModule(`package rec

s = true { t }
t = true { s }
a = true { b }
b = true { c }
c = true { d; e }
d = true { true }
e = true { a }`),
		"newMod2": MustParseModule(`package rec

x = true { s }`,
		),
		"newMod3": MustParseModule(`package rec2

import data.rec.x

y = true { x }`),
		"newMod4": MustParseModule(`package rec3

p[x] = y { data.rec4[x][y] = z }`,
		),
		"newMod5": MustParseModule(`package rec4

import data.rec3.p

q[x] = y { p[x] = y }`),
		"newMod6": MustParseModule(`package rec5

acp[x] { acq[x] }
acq[x] { a = [true | acp[_]]; a[_] = x }
`,
		),
		"newMod7": MustParseModule(`package rec6

np[x] = y { data.a[data.b.c[nq[x]]] = y }
nq[x] = y { data.d[data.e[x].f[np[y]]] }`,
		),
		"newMod8": MustParseModule(`package rec7

prefix = true { data.rec7 }`,
		),
		"newMod9": MustParseModule(`package rec8

dataref = true { data }`,
		),
		"newMod10": MustParseModule(`package rec9

		else_self { false } else { else_self }

		elsetop {
			false
		} else = elsemid {
			true
		}

		elsemid {
			false
		} else {
			elsebottom
		}

		elsebottom { elsetop }
		`),
		"fnMod1": MustParseModule(`package f0

		fn(x) = y {
			fn(x, y)
		}`),
		"fnMod2": MustParseModule(`package f1

		foo(x) = y {
			bar("buz", x, y)
		}

		bar(x, y) = z {
			foo([x, y], z)
		}`),
		"fnMod3": MustParseModule(`package f2

		foo(x) = y {
			bar("buz", x, y)
		}

		bar(x, y) = z {
			x = p[y]
			z = x
		}

		p[x] = y {
			x = "foo.bar"
			foo(x, y)
		}`),
	}

	compileStages(c, c.checkRecursion)

	makeRuleErrMsg := func(rule string, loop ...string) string {
		return fmt.Sprintf("rego_recursion_error: rule %v is recursive: %v", rule, strings.Join(loop, " -> "))
	}

	makeFuncErrMsg := func(fn string, loop ...string) string {
		return fmt.Sprintf("rego_recursion_error: func %v is recursive: %v", fn, strings.Join(loop, " -> "))
	}

	expected := []string{
		makeRuleErrMsg("s", "s", "t", "s"),
		makeRuleErrMsg("t", "t", "s", "t"),
		makeRuleErrMsg("a", "a", "b", "c", "e", "a"),
		makeRuleErrMsg("b", "b", "c", "e", "a", "b"),
		makeRuleErrMsg("c", "c", "e", "a", "b", "c"),
		makeRuleErrMsg("e", "e", "a", "b", "c", "e"),
		makeRuleErrMsg("p", "p", "q", "p"),
		makeRuleErrMsg("q", "q", "p", "q"),
		makeRuleErrMsg("acq", "acq", "acp", "acq"),
		makeRuleErrMsg("acp", "acp", "acq", "acp"),
		makeRuleErrMsg("np", "np", "nq", "np"),
		makeRuleErrMsg("nq", "nq", "np", "nq"),
		makeRuleErrMsg("prefix", "prefix", "prefix"),
		makeRuleErrMsg("dataref", "dataref", "dataref"),
		makeRuleErrMsg("else_self", "else_self", "else_self"),
		makeRuleErrMsg("elsetop", "elsetop", "elsemid", "elsebottom", "elsetop"),
		makeRuleErrMsg("elsemid", "elsemid", "elsebottom", "elsetop", "elsemid"),
		makeRuleErrMsg("elsebottom", "elsebottom", "elsetop", "elsemid", "elsebottom"),
		makeFuncErrMsg("fn", "fn", "fn"),
		makeFuncErrMsg("foo", "foo", "bar", "foo"),
		makeFuncErrMsg("bar", "bar", "foo", "bar"),
		makeFuncErrMsg("bar", "bar", "p", "foo", "bar"),
		makeFuncErrMsg("foo", "foo", "bar", "p", "foo"),
		makeRuleErrMsg("p", "p", "foo", "bar", "p"),
	}

	result := compilerErrsToStringSlice(c.Errors)
	sort.Strings(expected)

	if len(result) != len(expected) {
		t.Fatalf("Expected %d:\n%v\nBut got %d:\n%v", len(expected), strings.Join(expected, "\n"), len(result), strings.Join(result, "\n"))
	}

	for i := range result {
		if result[i] != expected[i] {
			t.Errorf("Expected %v but got: %v", expected[i], result[i])
		}
	}
}

func TestCompilerGetRulesExact(t *testing.T) {
	mods := getCompilerTestModules()

	// Add incrementally defined rules.
	mods["mod-incr"] = MustParseModule(`package a.b.c

p[1] { true }
p[2] { true }`,
	)

	c := NewCompiler()
	c.Compile(mods)
	assertNotFailed(t, c)

	tests := []struct {
		note     string
		ref      interface{}
		expected []*Rule
	}{
		{"exact", "data.a.b.c.p", []*Rule{
			c.Modules["mod-incr"].Rules[0],
			c.Modules["mod-incr"].Rules[1],
			c.Modules["mod1"].Rules[0],
		}},
		{"too short", "data.a", []*Rule{}},
		{"too long/not found", "data.a.b.c.p.q", []*Rule{}},
		{"outside data", "input.a.b.c.p", []*Rule{}},
		{"non-string/var", "data.a.b[data.foo]", []*Rule{}},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			var ref Ref
			switch r := tc.ref.(type) {
			case string:
				ref = MustParseRef(r)
			case Ref:
				ref = r
			}
			rules := c.GetRulesExact(ref)
			if len(rules) != len(tc.expected) {
				t.Fatalf("Expected exactly %v rules but got: %v", len(tc.expected), rules)
			}
			for i := range rules {
				found := false
				for j := range tc.expected {
					if rules[i].Equal(tc.expected[j]) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Expected exactly %v but got: %v", tc.expected, rules)
				}
			}
		})
	}
}

func TestCompilerGetRulesForVirtualDocument(t *testing.T) {
	mods := getCompilerTestModules()

	// Add incrementally defined rules.
	mods["mod-incr"] = MustParseModule(`package a.b.c

p[1] { true }
p[2] { true }`,
	)

	c := NewCompiler()
	c.Compile(mods)
	assertNotFailed(t, c)

	tests := []struct {
		note     string
		ref      interface{}
		expected []*Rule
	}{
		{"exact", "data.a.b.c.p", []*Rule{
			c.Modules["mod-incr"].Rules[0],
			c.Modules["mod-incr"].Rules[1],
			c.Modules["mod1"].Rules[0],
		}},
		{"deep", "data.a.b.c.p.q", []*Rule{
			c.Modules["mod-incr"].Rules[0],
			c.Modules["mod-incr"].Rules[1],
			c.Modules["mod1"].Rules[0],
		}},
		{"too short", "data.a", []*Rule{}},
		{"non-existent", "data.a.deadbeef", []*Rule{}},
		{"non-string/var", "data.a.b[data.foo]", []*Rule{}},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			var ref Ref
			switch r := tc.ref.(type) {
			case string:
				ref = MustParseRef(r)
			case Ref:
				ref = r
			}
			rules := c.GetRulesForVirtualDocument(ref)
			if len(rules) != len(tc.expected) {
				t.Fatalf("Expected exactly %v rules but got: %v", len(tc.expected), rules)
			}
			for i := range rules {
				found := false
				for j := range tc.expected {
					if rules[i].Equal(tc.expected[j]) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Expected exactly %v but got: %v", tc.expected, rules)
				}
			}
		})
	}
}

func TestCompilerGetRulesWithPrefix(t *testing.T) {
	mods := getCompilerTestModules()

	// Add incrementally defined rules.
	mods["mod-incr"] = MustParseModule(`package a.b.c

p[1] { true }
p[2] { true }
q[3] { true }`,
	)

	c := NewCompiler()
	c.Compile(mods)
	assertNotFailed(t, c)

	tests := []struct {
		note     string
		ref      interface{}
		expected []*Rule
	}{
		{"exact", "data.a.b.c.p", []*Rule{
			c.Modules["mod-incr"].Rules[0],
			c.Modules["mod-incr"].Rules[1],
			c.Modules["mod1"].Rules[0],
		}},
		{"too deep", "data.a.b.c.p.q", []*Rule{}},
		{"prefix", "data.a.b.c", []*Rule{
			c.Modules["mod1"].Rules[0],
			c.Modules["mod1"].Rules[1],
			c.Modules["mod1"].Rules[2],
			c.Modules["mod2"].Rules[0],
			c.Modules["mod-incr"].Rules[0],
			c.Modules["mod-incr"].Rules[1],
			c.Modules["mod-incr"].Rules[2],
		}},
		{"non-existent", "data.a.deadbeef", []*Rule{}},
		{"non-string/var", "data.a.b[data.foo]", []*Rule{}},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			var ref Ref
			switch r := tc.ref.(type) {
			case string:
				ref = MustParseRef(r)
			case Ref:
				ref = r
			}
			rules := c.GetRulesWithPrefix(ref)
			if len(rules) != len(tc.expected) {
				t.Fatalf("Expected exactly %v rules but got: %v", len(tc.expected), rules)
			}
			for i := range rules {
				found := false
				for j := range tc.expected {
					if rules[i].Equal(tc.expected[j]) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Expected %v but got: %v", tc.expected, rules)
				}
			}
		})
	}
}

func TestCompilerGetRules(t *testing.T) {
	compiler := getCompilerWithParsedModules(map[string]string{
		"mod1": `package a.b.c

p[x] = y { q[x] = y }
q["a"] = 1 { true }
q["b"] = 2 { true }`,
	})

	compileStages(compiler, nil)

	rule1 := compiler.Modules["mod1"].Rules[0]
	rule2 := compiler.Modules["mod1"].Rules[1]
	rule3 := compiler.Modules["mod1"].Rules[2]

	tests := []struct {
		input    string
		expected []*Rule
	}{
		{"data.a.b.c.p", []*Rule{rule1}},
		{"data.a.b.c.p.x", []*Rule{rule1}},
		{"data.a.b.c.q", []*Rule{rule2, rule3}},
		{"data.a.b.c", []*Rule{rule1, rule2, rule3}},
		{"data.a.b.d", nil},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.input, func(t *testing.T) {
			result := compiler.GetRules(MustParseRef(tc.input))
			for i := range result {
				found := false
				for j := range tc.expected {
					if result[i].Equal(tc.expected[j]) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Expected %v but got: %v", tc.expected, result)
				}
			}
		})
	}

}

func TestCompilerLazyLoadingError(t *testing.T) {

	testLoader := func(map[string]*Module) (map[string]*Module, error) {
		return nil, fmt.Errorf("something went horribly wrong")
	}

	compiler := NewCompiler().WithModuleLoader(testLoader)

	compiler.Compile(nil)

	expected := Errors{
		NewError(CompileErr, nil, "something went horribly wrong"),
	}

	if !reflect.DeepEqual(expected, compiler.Errors) {
		t.Fatalf("Expected error %v but got: %v", expected, compiler.Errors)
	}
}

func TestCompilerLazyLoading(t *testing.T) {

	mod1 := MustParseModule(`package a.b.c

import data.x.z1 as z2

p = true { q; r }
q = true { z2 }`)

	mod2 := MustParseModule(`package a.b.c

r = true { true }`)

	mod3 := MustParseModule(`package x

import data.foo.bar
import input.input

z1 = true { [localvar | count(bar.baz.qux, localvar)] }`)

	mod4 := MustParseModule(`package foo.bar.baz

qux = grault { true }`)

	mod5 := MustParseModule(`package foo.bar.baz

import data.d.e.f

deadbeef = f { true }
grault = deadbeef { true }`)

	// testLoader will return 4 rounds of parsed modules.
	rounds := []map[string]*Module{
		{"mod1": mod1, "mod2": mod2},
		{"mod3": mod3},
		{"mod4": mod4},
		{"mod5": mod5},
	}

	// For each round, run checks.
	tests := []func(map[string]*Module){
		func(map[string]*Module) {
			// first round, no modules because compiler is invoked with empty
			// collection.
		},
		func(partial map[string]*Module) {
			p := MustParseRule(`p = true { data.a.b.c.q; data.a.b.c.r }`)
			if !partial["mod1"].Rules[0].Equal(p) {
				t.Errorf("Expected %v but got %v", p, partial["mod1"].Rules[0])
			}
			q := MustParseRule(`q = true { data.x.z1 }`)
			if !partial["mod1"].Rules[1].Equal(q) {
				t.Errorf("Expected %v but got %v", q, partial["mod1"].Rules[0])
			}
		},
		func(partial map[string]*Module) {
			z1 := MustParseRule(`z1 = true { [localvar | count(data.foo.bar.baz.qux, localvar)] }`)
			if !partial["mod3"].Rules[0].Equal(z1) {
				t.Errorf("Expected %v but got %v", z1, partial["mod3"].Rules[0])
			}
		},
		func(partial map[string]*Module) {
			qux := MustParseRule(`qux = grault { true }`)
			if !partial["mod4"].Rules[0].Equal(qux) {
				t.Errorf("Expected %v but got %v", qux, partial["mod4"].Rules[0])
			}
		},
		func(partial map[string]*Module) {
			grault := MustParseRule(`qux = data.foo.bar.baz.grault { true }`) // rewrite has not happened yet
			f := MustParseRule(`deadbeef = data.d.e.f { true }`)
			if !partial["mod4"].Rules[0].Equal(grault) {
				t.Errorf("Expected %v but got %v", grault, partial["mod4"].Rules[0])
			}
			if !partial["mod5"].Rules[0].Equal(f) {
				t.Errorf("Expected %v but got %v", f, partial["mod5"].Rules[0])
			}
		},
	}

	round := 0

	testLoader := func(modules map[string]*Module) (map[string]*Module, error) {
		tests[round](modules)
		if round >= len(rounds) {
			return nil, nil
		}
		result := rounds[round]
		round++
		return result, nil
	}

	compiler := NewCompiler().WithModuleLoader(testLoader)

	if compiler.Compile(nil); compiler.Failed() {
		t.Fatalf("Got unexpected error from compiler: %v", compiler.Errors)
	}
}

func TestQueryCompiler(t *testing.T) {
	tests := []struct {
		note     string
		q        string
		pkg      string
		imports  []string
		input    string
		expected interface{}
	}{
		{"exports resolved", "z", `package a.b.c`, nil, "", "data.a.b.c.z"},
		{"imports resolved", "z", `package a.b.c.d`, []string{"import data.a.b.c.z"}, "", "data.a.b.c.z"},
		{"unsafe vars", "z", "", nil, "", fmt.Errorf("1 error occurred: 1:1: rego_unsafe_var_error: var z is unsafe")},
		{"safe vars", `data; abc`, `package ex`, []string{"import input.xyz as abc"}, `{}`, `data; input.xyz`},
		{"reorder", `x != 1; x = 0`, "", nil, "", `x = 0; x != 1`},
		{"bad with target", "x = 1 with data.p as null", "", nil, "", fmt.Errorf("1 error occurred: 1:7: rego_type_error: with keyword target must be input")},
		{"check types", "x = data.a.b.c.z; y = null; x = y", "", nil, "", fmt.Errorf("match error\n\tleft  : number\n\tright : null")},
	}

	for _, tc := range tests {
		runQueryCompilerTest(t, tc.note, tc.q, tc.pkg, tc.imports, tc.input, tc.expected)
	}
}

func assertCompilerErrorStrings(t *testing.T, compiler *Compiler, expected []string) {
	result := compilerErrsToStringSlice(compiler.Errors)
	if len(result) != len(expected) {
		t.Fatalf("Expected %d:\n%v\nBut got %d:\n%v", len(expected), strings.Join(expected, "\n"), len(result), strings.Join(result, "\n"))
	}
	for i := range result {
		if !strings.Contains(result[i], expected[i]) {
			t.Errorf("Expected %v but got: %v", expected[i], result[i])
		}
	}
}

func assertNotFailed(t *testing.T, c *Compiler) {
	if c.Failed() {
		t.Errorf("Unexpected compilation error: %v", c.Errors)
	}
}

func assertFailed(t *testing.T, c *Compiler) {
	if !c.Failed() {
		t.Error("Expected compilation error.")
	}
}

func getCompilerWithParsedModules(mods map[string]string) *Compiler {

	parsed := map[string]*Module{}

	for id, input := range mods {
		mod, err := ParseModule(id, input)
		if err != nil {
			panic(err)
		}
		parsed[id] = mod
	}

	compiler := NewCompiler()
	compiler.Modules = parsed

	return compiler
}

// helper function to run compiler upto given stage. If nil is provided, a
// normal compile run is performed.
func compileStages(c *Compiler, upto func()) {
	if upto == nil {
		c.compile()
		return
	}
	target := reflect.ValueOf(upto)
	for _, fn := range c.stages {
		if fn(); c.Failed() {
			return
		}
		if reflect.ValueOf(fn).Pointer() == target.Pointer() {
			break
		}
	}
}

func getCompilerTestModules() map[string]*Module {

	mod1 := MustParseModule(`package a.b.c

import data.x.y.z as foo
import data.g.h.k

p[x] { q[x]; not r[x] }
q[x] { foo[i] = x }
z = 400 { true }`,
	)

	mod2 := MustParseModule(`package a.b.c

import data.bar
import data.x.y.p

r[x] { bar[x] = 100; p = 101 }`)

	mod3 := MustParseModule(`package a.b.d

import input.x as y

t = true { input = {y.secret: [{y.keyid}]} }
x = false { true }`)

	mod4 := MustParseModule(`package a.b.empty`)

	mod5 := MustParseModule(`package a.b.compr

import input.x as y
import data.a.b.c.q

p = true { [y.a | true] }
r = true { [q.a | true] }
s = true { [true | y.a = 0] }
t = true { [true | q[i] = 1] }
u = true { [true | _ = [y.a | true]] }
v = true { [true | _ = [true | q[i] = 1]] }`,
	)

	mod6 := MustParseModule(`package a.b.nested

import data.x
import data.z
import input.x as y

p = true { x[y[i].a[z.b[j]]] }
q = true { x = v; v[y[i]] }
r = 1 { true }
s = true { x[r] }`,
	)

	mod7 := MustParseModule(`package a.b.funcs

fn(x) = y {
	trim(x, ".", y)
}

bar([x, y]) = [a, [b, c]] {
	fn(x, a)
	y[1].b = b
	y[i].a = "hi"
	c = y[i].b
}

foorule = true {
	bar(["hi.there", [{"a": "hi", "b": 1}, {"a": "bye", "b": 0}]], [a, [b, c]])
}`)

	return map[string]*Module{
		"mod1": mod1,
		"mod2": mod2,
		"mod3": mod3,
		"mod4": mod4,
		"mod5": mod5,
		"mod6": mod6,
		"mod7": mod7,
	}
}

func compilerErrsToStringSlice(errors []*Error) []string {
	result := []string{}
	for _, e := range errors {
		msg := strings.SplitN(e.Error(), ":", 3)[2]
		result = append(result, strings.TrimSpace(msg))
	}
	sort.Strings(result)
	return result
}

func runQueryCompilerTest(t *testing.T, note, q, pkg string, imports []string, input string, expected interface{}) {
	test.Subtest(t, note, func(t *testing.T) {
		c := NewCompiler()
		c.Compile(getCompilerTestModules())
		assertNotFailed(t, c)
		qc := c.QueryCompiler()
		query := MustParseBody(q)
		var qctx *QueryContext

		if pkg != "" {
			qctx = qctx.WithPackage(MustParsePackage(pkg))
		}
		if len(imports) != 0 {
			qctx = qctx.WithImports(MustParseImports(strings.Join(imports, "\n")))
		}
		if input != "" {
			qctx = qctx.WithInput(MustParseTerm(input).Value)
		}

		if qctx != nil {
			qc.WithContext(qctx)
		}

		switch expected := expected.(type) {
		case string:
			expectedQuery := MustParseBody(expected)
			result, err := qc.Compile(query)
			if err != nil {
				t.Fatalf("Unexpected error from %v: %v", query, err)
			}
			if !expectedQuery.Equal(result) {
				t.Fatalf("Expected:\n%v\n\nGot:\n%v", expectedQuery, result)
			}
		case error:
			result, err := qc.Compile(query)
			if err == nil {
				t.Fatalf("Expected error from %v but got: %v", query, result)
			}
			if !strings.Contains(err.Error(), expected.Error()) {
				t.Fatalf("Expected error %v but got: %v", expected, err)
			}
		}
	})
}
