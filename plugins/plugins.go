// Copyright 2018 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package plugins implements plugin management for the policy engine.
package plugins

import (
	"context"
	"encoding/json"

	"sync"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins/rest"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"
)

// Plugin defines the interface for OPA plugins.
type Plugin interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
}

// Manager implements lifecycle management of plugins and gives plugins access
// to engine-wide components like storage.
type Manager struct {
	Labels   map[string]string
	Store    storage.Store
	Compiler *ast.Compiler
	services map[string]rest.Client
	plugins  []Plugin
	mtx      sync.RWMutex
}

// New creates a new Manager using config.
func New(config []byte, id string, store storage.Store) (*Manager, error) {

	var parsedConfig struct {
		Services []json.RawMessage
		Labels   map[string]string
	}

	if err := util.Unmarshal(config, &parsedConfig); err != nil {
		return nil, err
	}

	if parsedConfig.Labels == nil {
		parsedConfig.Labels = map[string]string{}
	}

	services := map[string]rest.Client{}

	for _, s := range parsedConfig.Services {
		client, err := rest.New(s)
		if err != nil {
			return nil, err
		}
		services[client.Service()] = client
	}

	parsedConfig.Labels["id"] = id

	m := &Manager{
		Labels:   parsedConfig.Labels,
		Store:    store,
		services: services,
	}

	return m, nil
}

// Register adds a plugin to the manager. When the manager is started, all of
// the plugins will be started.
func (m *Manager) Register(plugin Plugin) {
	m.plugins = append(m.plugins, plugin)
}

func (m *Manager) GetCompiler() *ast.Compiler {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	return m.Compiler
}

func (m *Manager) setCompiler(compiler *ast.Compiler) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.Compiler = compiler
}

// Start starts the manager.
func (m *Manager) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}

	// open transaction
	txn, err := m.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return err
	}
	compiler, err := loadCompilerFromStore(m.Store, txn, ctx)
	if err != nil {
		return err
	}

	// set compiler on manager
	m.setCompiler(compiler)

	for _, p := range m.plugins {
		if err := p.Start(ctx); err != nil {
			return err
		}
	}

	storage.Txn(ctx, m.Store, storage.WriteParams, func(txn storage.Transaction) error {
		m.Store.Register(ctx, txn, storage.TriggerConfig{OnCommit: func(ctx context.Context, txn storage.Transaction, event storage.TriggerEvent) {
			if event.PolicyChanged() {
				compiler, _ := loadCompilerFromStore(m.Store, txn, ctx)
				m.setCompiler(compiler)
			}
		}})

		return nil
	})

	return nil
}

func loadCompilerFromStore(store storage.Store, txn storage.Transaction, ctx context.Context) (*ast.Compiler, error) {
	policies, err := store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, err
	}
	modules := map[string]*ast.Module{}

	for _, policy := range policies {
		bs, err := store.GetPolicy(ctx, txn, policy)
		if err != nil {
			return nil, err
		}
		module, err := ast.ParseModule(policy, string(bs))
		if err != nil {
			return nil, err
		}
		modules[policy] = module
	}

	compiler := ast.NewCompiler()
	compiler.Compile(modules)
	return compiler, nil
}

// Client returns a client for communicating with a remote service.
func (m *Manager) Client(name string) rest.Client {
	return m.services[name]
}

// Services returns a list of services that m can provide clients for.
func (m *Manager) Services() []string {
	s := make([]string, 0, len(m.services))
	for name := range m.services {
		s = append(s, name)
	}
	return s
}
