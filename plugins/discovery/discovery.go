// Copyright 2018 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package discovery implements configuration discovery.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	bundleApi "github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/download"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/bundle"
	"github.com/open-policy-agent/opa/plugins/logs"
	"github.com/open-policy-agent/opa/plugins/status"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage/inmem"
	"github.com/sirupsen/logrus"
)

// Discovery implements configuration discovery for OPA. When discovery is
// started it will periodically download a configuration bundle and try to
// reconfigure the OPA.
type Discovery struct {
	manager    *plugins.Manager
	config     *Config
	initfuncs  map[string]plugins.PluginInitFunc // factory functions for custom plugins
	downloader *download.Downloader              // discovery bundle downloader
	status     *bundle.Status                    // discovery status
	etag       string                            // discovery bundle etag for caching purposes
}

// CustomPlugins provides a set of factory functions to use for instantiating
// custom plugins.
func CustomPlugins(funcs map[string]plugins.PluginInitFunc) func(*Discovery) {
	return func(d *Discovery) {
		d.initfuncs = funcs
	}
}

// New returns a new discovery plugin.
func New(manager *plugins.Manager, opts ...func(*Discovery)) (*Discovery, error) {

	result := &Discovery{
		manager: manager,
	}

	for _, f := range opts {
		f(result)
	}

	config, err := ParseConfig(manager.Config.Discovery, manager.Services())

	if err != nil {
		return nil, err
	} else if config == nil {
		if _, err := getPluginSet(result.initfuncs, manager, manager.Config); err != nil {
			return nil, err
		}
		return result, nil
	}

	if manager.Config.PluginsEnabled() {
		return nil, fmt.Errorf("plugins cannot be specified in the bootstrap configuration when discovery enabled")
	}

	result.config = config
	result.downloader = download.New(config.Config, manager.Client(config.service), config.path).WithCallback(result.oneShot)
	result.status = &bundle.Status{
		Name: *config.Name,
	}

	return result, nil
}

// Start starts the dynamic discovery process if configured.
func (c *Discovery) Start(ctx context.Context) error {
	if c.downloader != nil {
		c.downloader.Start(ctx)
	}
	return nil
}

// Stop stops the dynamic discovery process if configured.
func (c *Discovery) Stop(ctx context.Context) {
	if c.downloader != nil {
		c.downloader.Stop(ctx)
	}
}

// Reconfigure is a no-op on discovery.
func (c *Discovery) Reconfigure(_ context.Context, _ interface{}) {

}

func (c *Discovery) oneShot(ctx context.Context, u download.Update) {

	c.processUpdate(ctx, u)

	if p := status.Lookup(c.manager); p != nil {
		p.UpdateDiscoveryStatus(*c.status)
	}
}

func (c *Discovery) processUpdate(ctx context.Context, u download.Update) {

	if u.Error != nil {
		c.logError("Discovery download failed: %v", u.Error)
		c.status.SetError(u.Error)
		return
	}

	if u.Bundle != nil {
		c.status.SetDownloadSuccess()

		if err := c.reconfigure(ctx, u); err != nil {
			c.logError("Discovery reconfiguration error occurred: %v", err)
			c.status.SetError(err)
			return
		}

		c.status.SetError(nil)
		c.status.SetActivateSuccess(u.Bundle.Manifest.Revision)
		if u.ETag != "" {
			c.logInfo("Discovery update processed successfully. Etag updated to %v.", u.ETag)
		} else {
			c.logInfo("Discovery update processed successfully.")
		}
		c.etag = u.ETag
		return
	}

	if u.ETag == c.etag {
		c.logError("Discovery update skipped, server replied with not modified.")
		c.status.SetError(nil)
		return
	}
}

func (c *Discovery) reconfigure(ctx context.Context, u download.Update) error {

	config, ps, err := processBundle(ctx, c.manager, c.initfuncs, u.Bundle, c.config.query)
	if err != nil {
		return err
	}

	if err := c.manager.Reconfigure(config); err != nil {
		return err
	}

	// TODO(tsandall): we don't currently support changes to discovery
	// configuration. These changes are risky because errors would be
	// unrecoverable (without keeping track of changes and rolling back...)

	// TODO(tsandall): add protection against discovery -service- changing.
	for _, p := range ps.Start {
		if err := p.Start(ctx); err != nil {
			return err
		}
	}

	for _, p := range ps.Reconfig {
		p.Plugin.Reconfigure(ctx, p.Config)
	}

	return nil
}

func (c *Discovery) logError(fmt string, a ...interface{}) {
	logrus.WithFields(c.logrusFields()).Errorf(fmt, a...)
}

func (c *Discovery) logInfo(fmt string, a ...interface{}) {
	logrus.WithFields(c.logrusFields()).Infof(fmt, a...)
}

func (c *Discovery) logDebug(fmt string, a ...interface{}) {
	logrus.WithFields(c.logrusFields()).Debugf(fmt, a...)
}

func (c *Discovery) logrusFields() logrus.Fields {
	return logrus.Fields{
		"name":   *c.config.Name,
		"plugin": "discovery",
	}
}

func processBundle(ctx context.Context, manager *plugins.Manager, initfuncs map[string]plugins.PluginInitFunc, b *bundleApi.Bundle, query string) (*config.Config, *pluginSet, error) {

	config, err := evaluateBundle(ctx, manager.ID, manager.Info, b, query)
	if err != nil {
		return nil, nil, err
	}

	ps, err := getPluginSet(initfuncs, manager, config)
	return config, ps, err
}

func evaluateBundle(ctx context.Context, id string, info *ast.Term, b *bundleApi.Bundle, query string) (*config.Config, error) {

	modules := map[string]*ast.Module{}

	for _, file := range b.Modules {
		modules[file.Path] = file.Parsed
	}

	compiler := ast.NewCompiler()

	if compiler.Compile(modules); compiler.Failed() {
		return nil, compiler.Errors
	}

	store := inmem.NewFromObject(b.Data)

	rego := rego.New(
		rego.Query(query),
		rego.Compiler(compiler),
		rego.Store(store),
		rego.Runtime(info),
	)

	rs, err := rego.Eval(ctx)
	if err != nil {
		return nil, err
	}

	if len(rs) == 0 {
		return nil, fmt.Errorf("undefined configuration")
	}

	bs, err := json.Marshal(rs[0].Expressions[0].Value)
	if err != nil {
		return nil, err
	}

	return config.ParseConfig(bs, id)
}

type pluginSet struct {
	Start    []plugins.Plugin
	Reconfig []pluginreconfig
}

type pluginreconfig struct {
	Config interface{}
	Plugin plugins.Plugin
}

func getPluginSet(initfuncs map[string]plugins.PluginInitFunc, manager *plugins.Manager, config *config.Config) (*pluginSet, error) {

	bundleConfig, err := bundle.ParseConfig(config.Bundle, manager.Services())
	if err != nil {
		return nil, err
	}

	decisionLogsConfig, err := logs.ParseConfig(config.DecisionLogs, manager.Services())
	if err != nil {
		return nil, err
	}

	statusConfig, err := status.ParseConfig(config.Status, manager.Services())
	if err != nil {
		return nil, err
	}

	starts := []plugins.Plugin{}
	reconfigs := []pluginreconfig{}

	if bundleConfig != nil {
		p, created := getBundlePlugin(manager, bundleConfig)
		if created {
			starts = append(starts, p)
		} else if p != nil {
			reconfigs = append(reconfigs, pluginreconfig{bundleConfig, p})
		}
	}

	if decisionLogsConfig != nil {
		p, created := getDecisionLogsPlugin(manager, decisionLogsConfig)
		if created {
			starts = append(starts, p)
		} else if p != nil {
			reconfigs = append(reconfigs, pluginreconfig{decisionLogsConfig, p})
		}
	}

	if statusConfig != nil {
		p, created := getStatusPlugin(manager, statusConfig)
		if created {
			starts = append(starts, p)
		} else if p != nil {
			reconfigs = append(reconfigs, pluginreconfig{statusConfig, p})
		}
	}

	result := &pluginSet{starts, reconfigs}

	return result, getCustomPlugins(initfuncs, manager, config.Plugins, result)
}

func getBundlePlugin(m *plugins.Manager, config *bundle.Config) (plugin *bundle.Plugin, created bool) {
	plugin = bundle.Lookup(m)
	if plugin == nil {
		plugin = bundle.New(config, m)
		m.Register(bundle.Name, plugin)
		registerBundleStatusUpdates(m)
		created = true
	}
	return plugin, created
}

func getDecisionLogsPlugin(m *plugins.Manager, config *logs.Config) (plugin *logs.Plugin, created bool) {
	plugin = logs.Lookup(m)
	if plugin == nil {
		plugin = logs.New(config, m)
		m.Register(logs.Name, plugin)
		created = true
	}
	return plugin, created
}

func getStatusPlugin(m *plugins.Manager, config *status.Config) (plugin *status.Plugin, created bool) {

	plugin = status.Lookup(m)

	if plugin == nil {
		plugin = status.New(config, m)
		m.Register(status.Name, plugin)
		registerBundleStatusUpdates(m)
		created = true
	}

	return plugin, created
}

func getCustomPlugins(initfuncs map[string]plugins.PluginInitFunc, manager *plugins.Manager, configs map[string]json.RawMessage, result *pluginSet) error {

	for name, config := range configs {
		plugin := manager.Plugin(name)
		if plugin == nil {
			// TODO: report missing plugin initfunc to controller...
			if f, ok := initfuncs[name]; ok {
				plugin, err := f(manager, config)
				if err != nil {
					return err
				} else if plugin == nil {
					return fmt.Errorf("plugin %q returned nil object", name)
				}
				manager.Register(name, plugin)
				result.Start = append(result.Start, plugin)
			}
		} else {
			result.Reconfig = append(result.Reconfig, pluginreconfig{config, plugin})
		}
	}

	return nil
}

func registerBundleStatusUpdates(m *plugins.Manager) {
	bp := bundle.Lookup(m)
	sp := status.Lookup(m)
	if bp == nil || sp == nil {
		return
	}
	type pluginlistener string
	bp.Register(pluginlistener(status.Name), func(s bundle.Status) {
		sp.UpdateBundleStatus(s)
	})
}
