// Copyright 2018 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// +build linux,cgo darwin,cgo

package runtime

// Contains parts of the runtime package that use the plugin package

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/topdown"
)

// RegisterSharedObjectsFromDir registers all .builtin.so and .plugin.so files recursively stored in dir into OPA.
func RegisterSharedObjectsFromDir(dir string) error {
	return filepath.Walk(dir, loadSharedObjectWalker())
}

// loadSharedObjectWalker returns a walkfunc that registers every .builtin.so file and every .plugin.so file into OPA.
// Ignores all other file types.
func loadSharedObjectWalker() filepath.WalkFunc {
	walk := func(path string, f os.FileInfo, err error) error {
		// if error occurs during traversal to path, exit and crash
		if err != nil {
			return err
		}
		// skip anything that is a directory
		if f.IsDir() {
			return nil
		}

		// call builtin function if is builtin
		if isBuiltin, _ := filepath.Match("*.builtin.so", filepath.Base(path)); isBuiltin {
			return registerBuiltinFromFile(path)
		}

		// call plugin function if is plugin
		if isPlugin, _ := filepath.Match("*.plugin.so", filepath.Base(path)); isPlugin {
			return registerPluginFromFile(path)
		}

		// ignore anything else
		return nil
	}
	return walk
}

// loads the builtin from a file path
func registerBuiltinFromFile(path string) error {
	mod, err := plugin.Open(path)
	if err != nil {
		return err
	}
	builtinSym, err := mod.Lookup("Builtin")
	if err != nil {
		return err
	}
	functionSym, err := mod.Lookup("Function")
	if err != nil {
		return err
	}

	// type assert builtin symbol
	builtin, ok := builtinSym.(*ast.Builtin)
	if !ok {
		return fmt.Errorf("symbol Builtin must be of type ast.Builtin")
	}

	// type assert function symbol
	switch fnc := functionSym.(type) {
	case *topdown.BuiltinFunc:
		ast.RegisterBuiltin(builtin)
		topdown.RegisterBuiltinFunc(builtin.Name, *fnc)
	case *topdown.FunctionalBuiltin1:
		ast.RegisterBuiltin(builtin)
		topdown.RegisterFunctionalBuiltin1(builtin.Name, *fnc)
	case *topdown.FunctionalBuiltin2:
		ast.RegisterBuiltin(builtin)
		topdown.RegisterFunctionalBuiltin2(builtin.Name, *fnc)
	case *topdown.FunctionalBuiltin3:
		ast.RegisterBuiltin(builtin)
		topdown.RegisterFunctionalBuiltin3(builtin.Name, *fnc)
	default:
		return fmt.Errorf("symbol Function was of an unrecognized type")
	}

	// TODO: replace with logging
	fmt.Printf("Registered builtin %v from %v\n", builtin.Name, path)
	return nil
}

// loads a plugin from a file path
func registerPluginFromFile(path string) error {
	mod, err := plugin.Open(path)
	if err != nil {
		return err
	}

	nameSym, err := mod.Lookup("Name")
	if err != nil {
		return err
	}
	name, ok := nameSym.(*string)
	if !ok {
		return fmt.Errorf("symbol Name must be of type string")
	}

	pluginSym, err := mod.Lookup("Initializer")
	if err != nil {
		return err
	}

	// type assert initializer function
	initFunc, ok := pluginSym.(*plugins.PluginInitFunc)
	if !ok {
		return fmt.Errorf("symbol Builtin must be of type runtime.PluginInitFunc")
	}

	//TODO: replace this with appropriate logging method
	fmt.Printf("Registered plugin %v from %v\n", name, path)
	RegisterPlugin(*name, *initFunc)
	return nil
}
