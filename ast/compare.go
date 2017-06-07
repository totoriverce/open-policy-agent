// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/open-policy-agent/opa/util"
)

// Compare returns an integer indicating whether two AST values are less than,
// equal to, or greater than each other.
//
// If a is less than b, the return value is negative. If a is greater than b,
// the return value is positive. If a is equal to b, the return value is zero.
//
// Different types are never equal to each other. For comparison purposes, types
// are sorted as follows:
//
// nil < Null < Boolean < Number < String < Var < Ref < Array < Object < Set <
// ArrayComprehension < Expr < Body < Rule < Import < Package < Module.
//
// Arrays and Refs are equal iff both a and b have the same length and all
// corresponding elements are equal. If one element is not equal, the return
// value is the same as for the first differing element. If all elements are
// equal but a and b have different lengths, the shorter is considered less than
// the other.
//
// Objects are considered equal iff both a and b have the same sorted (key,
// value) pairs and are of the same length. Other comparisons are consistent but
// not defined.
//
// Sets are considered equal iff the symmetric difference of a and b is empty.
// Other comparisons are consistent but not defined.
func Compare(a, b interface{}) int {

	if t, ok := a.(*Term); ok {
		if t == nil {
			return Compare(nil, b)
		}
		return Compare(t.Value, b)
	}

	if t, ok := b.(*Term); ok {
		if t == nil {
			return Compare(a, nil)
		}
		return Compare(a, t.Value)
	}

	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}

	sortA := sortOrder(a)
	sortB := sortOrder(b)

	if sortA < sortB {
		return -1
	} else if sortB < sortA {
		return 1
	}

	switch a := a.(type) {
	case Null:
		return 0
	case Boolean:
		b := b.(Boolean)
		if a.Equal(b) {
			return 0
		}
		if !a {
			return -1
		}
		return 1
	case Number:
		return util.Compare(json.Number(a), json.Number(b.(Number)))
	case String:
		b := b.(String)
		if a.Equal(b) {
			return 0
		}
		if a < b {
			return -1
		}
		return 1
	case Var:
		b := b.(Var)
		if a.Equal(b) {
			return 0
		}
		if a < b {
			return -1
		}
		return 1
	case Ref:
		b := b.(Ref)
		return termSliceCompare(a, b)
	case Array:
		b := b.(Array)
		return termSliceCompare(a, b)
	case Object:
		b := b.(Object)
		keysA := a.Keys()
		keysB := b.Keys()
		sort.Sort(termSlice(keysA))
		sort.Sort(termSlice(keysB))
		minLen := len(a)
		if len(b) < len(a) {
			minLen = len(b)
		}
		for i := 0; i < minLen; i++ {
			keysCmp := Compare(keysA[i], keysB[i])
			if keysCmp < 0 {
				return -1
			}
			if keysCmp > 0 {
				return 1
			}
			valA := a.Get(keysA[i])
			valB := b.Get(keysB[i])
			valCmp := Compare(valA, valB)
			if valCmp != 0 {
				return valCmp
			}
		}
		if len(a) < len(b) {
			return -1
		}
		if len(b) < len(a) {
			return 1
		}
		return 0
	case *Set:
		b := b.(*Set)
		sort.Sort(termSlice(*a))
		sort.Sort(termSlice(*b))
		return termSliceCompare(*a, *b)
	case *ArrayComprehension:
		b := b.(*ArrayComprehension)
		if cmp := Compare(a.Term, b.Term); cmp != 0 {
			return cmp
		}
		return Compare(a.Body, b.Body)
	case *Expr:
		b := b.(*Expr)
		return a.Compare(b)
	case *With:
		b := b.(*With)
		return a.Compare(b)
	case Body:
		b := b.(Body)
		return a.Compare(b)
	case *Head:
		b := b.(*Head)
		return a.Compare(b)
	case *Rule:
		b := b.(*Rule)
		return a.Compare(b)
	case *Import:
		b := b.(*Import)
		return a.Compare(b)
	case *Package:
		b := b.(*Package)
		return a.Compare(b)
	case *Module:
		b := b.(*Module)
		return a.Compare(b)
	}
	panic(fmt.Sprintf("illegal value: %T", a))
}

type termSlice []*Term

func (s termSlice) Less(i, j int) bool { return Compare(s[i].Value, s[j].Value) < 0 }
func (s termSlice) Swap(i, j int)      { x := s[i]; s[i] = s[j]; s[j] = x }
func (s termSlice) Len() int           { return len(s) }

func sortOrder(x interface{}) int {
	switch x.(type) {
	case Null:
		return 0
	case Boolean:
		return 1
	case Number:
		return 2
	case String:
		return 3
	case Var:
		return 4
	case Ref:
		return 5
	case Array:
		return 6
	case Object:
		return 7
	case *Set:
		return 8
	case *ArrayComprehension:
		return 9
	case *Expr:
		return 100
	case *With:
		return 110
	case *Head:
		return 120
	case Body:
		return 200
	case *Rule:
		return 1000
	case *Import:
		return 1001
	case *Package:
		return 1002
	case *Module:
		return 10000
	}
	panic(fmt.Sprintf("illegal value: %T", x))
}

func importsCompare(a, b []*Import) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if cmp := a[i].Compare(b[i]); cmp != 0 {
			return cmp
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(b) < len(a) {
		return 1
	}
	return 0
}

func rulesCompare(a, b []*Rule) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if cmp := a[i].Compare(b[i]); cmp != 0 {
			return cmp
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(b) < len(a) {
		return 1
	}
	return 0
}

func termSliceCompare(a, b []*Term) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if cmp := Compare(a[i], b[i]); cmp != 0 {
			return cmp
		}
	}
	if len(a) < len(b) {
		return -1
	} else if len(b) < len(a) {
		return 1
	}
	return 0
}

func withSliceCompare(a, b []*With) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if cmp := Compare(a[i], b[i]); cmp != 0 {
			return cmp
		}
	}
	if len(a) < len(b) {
		return -1
	} else if len(b) < len(a) {
		return 1
	}
	return 0
}
