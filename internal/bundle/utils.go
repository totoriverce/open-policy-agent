// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package bundle

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/resolver/wasm"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"
)

// LoadWasmResolversFromStore will lookup all Wasm modules from the store along with the
// associated bundle manifest configuration and instantiate the respective resolvers.
func LoadWasmResolversFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, otherBundles map[string]*bundle.Bundle) ([]*wasm.Resolver, error) {
	bundleNames, err := bundle.ReadBundleNamesFromStore(ctx, store, txn)
	if err != nil && !storage.IsNotFound(err) {
		return nil, err
	}

	var resolversToLoad []*bundle.WasmModuleFile
	for _, bundleName := range bundleNames {
		var wasmResolverConfigs []bundle.WasmResolver
		rawModules := map[string][]byte{}

		// Save round-tripping the bundle that was just activated
		if _, ok := otherBundles[bundleName]; ok {
			wasmResolverConfigs = otherBundles[bundleName].Manifest.WasmResolvers
			for _, wmf := range otherBundles[bundleName].WasmModules {
				rawModules[wmf.Path] = wmf.Raw
			}
		} else {
			wasmResolverConfigs, err = bundle.ReadWasmMetadataFromStore(ctx, store, txn, bundleName)
			if err != nil && !storage.IsNotFound(err) {
				return nil, fmt.Errorf("failed to read wasm module manifest from store: %s", err)
			}
			rawModules, err = bundle.ReadWasmModulesFromStore(ctx, store, txn, bundleName)
			if err != nil && !storage.IsNotFound(err) {
				return nil, fmt.Errorf("failed to read wasm modules from store: %s", err)
			}
		}

		for path, raw := range rawModules {
			wmf := &bundle.WasmModuleFile{
				URL:  path,
				Path: path,
				Raw:  raw,
			}
			for _, resolverConf := range wasmResolverConfigs {
				if resolverConf.Module == path {
					ref, err := ast.PtrRef(ast.DefaultRootDocument, resolverConf.Entrypoint)
					if err != nil {
						return nil, fmt.Errorf("failed to parse wasm module entrypoint '%s': %s", resolverConf.Entrypoint, err)
					}
					wmf.Entrypoints = append(wmf.Entrypoints, ref)
				}
			}
			if len(wmf.Entrypoints) > 0 {
				resolversToLoad = append(resolversToLoad, wmf)
			}
		}
	}

	var resolvers []*wasm.Resolver
	if len(resolversToLoad) > 0 {
		// Get a full snapshot of the current data (including any from "outside" the bundles)
		data, err := store.Read(ctx, txn, storage.Path{})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize wasm runtime: %s", err)
		}

		for _, wmf := range resolversToLoad {
			resolver, err := wasm.New(wmf.Entrypoints, wmf.Raw, data)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize wasm module for entrypoints '%s': %s", wmf.Entrypoints, err)
			}
			resolvers = append(resolvers, resolver)
		}
	}
	return resolvers, nil
}

// LoadBundleFromDisk loads a previously persisted activated bundle from disk
func LoadBundleFromDisk(path, name string, bvc *bundle.VerificationConfig) (*bundle.Bundle, error) {
	bundlePath := filepath.Join(path, name, "bundle.tar.gz")
	manifestPath := filepath.Join(path, name, "manifest.json")

	if _, err := os.Stat(bundlePath); err == nil {
		f, err := os.Open(filepath.Join(bundlePath))
		if err != nil {
			return nil, err
		}
		defer f.Close()

		r := bundle.NewCustomReader(bundle.NewTarballLoaderWithBaseURL(f, ""))

		if _, err := os.Stat(manifestPath); err == nil {
			f, err := os.Open(filepath.Join(manifestPath))
			if err != nil {
				return nil, fmt.Errorf("failed to open manifest file: %w", err)
			}
			defer f.Close()

			var d map[string]interface{}
			if err := util.NewJSONDecoder(f).Decode(&d); err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed to decode manifest file: %w", err)
			}

			if etag, ok := d["etag"].(string); ok {
				r.WithBundleEtag(etag)
			}
		}

		if bvc != nil {
			r = r.WithBundleVerificationConfig(bvc)
		}

		b, err := r.Read()
		if err != nil {
			return nil, err
		}

		return &b, nil
	} else if os.IsNotExist(err) {
		return nil, nil
	} else {
		return nil, err
	}
}

// SaveBundleToDisk saves the given raw bytes representing the bundle's content to disk. Passing nil for the rawManifest
// will result in no manifest being persisted to disk.
func SaveBundleToDisk(path string, rawBundle io.Reader, rawManifest io.Reader) (string, string, error) {
	var cleanupOperations []func()
	var failed bool
	defer func() {
		if failed {
			for _, cleanup := range cleanupOperations {
				cleanup()
			}
		}
	}()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		err = os.MkdirAll(path, os.ModePerm)
		if err != nil {
			return "", "", err
		}
	}

	// supplying no bundle data is an error case
	if rawBundle == nil {
		return "", "", fmt.Errorf("no raw bundle bytes to persist to disk")
	}

	// create a temporary file to write the bundle to
	destBundle, err := os.CreateTemp(path, ".bundle.tar.gz.*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temporary bundle file: %w", err)
	}
	defer destBundle.Close()

	cleanupOperations = append(cleanupOperations, func() {
		os.Remove(destBundle.Name())
	})

	// handle the optional case where a bundle manifest is provided
	var errManifest error
	var destManifestName string
	if rawManifest != nil {
		destManifest, err := os.CreateTemp(path, ".manifest.json.*.tmp")
		if err != nil {
			return "", "", err
		}
		defer destManifest.Close()
		cleanupOperations = append(cleanupOperations, func() {
			os.Remove(destManifest.Name())
		})

		destManifestName = destManifest.Name()

		// write the manifest to disk first
		_, errManifest = io.Copy(destManifest, rawManifest)
	}

	// write the bundle to disk
	_, errBundle := io.Copy(destBundle, rawBundle)

	// handle errors from both the bundle or manifest write operations
	if errBundle != nil && errManifest != nil {
		failed = true
		return "", "", fmt.Errorf("failed to save bundle and manifest to disk: %s, %s", errBundle, errManifest)
	}
	if errBundle != nil {
		failed = true
		return "", "", fmt.Errorf("failed to save bundle to disk: %s", errBundle)
	}
	if errManifest != nil {
		failed = true
		return "", "", fmt.Errorf("failed to save manifest to disk: %s", errManifest)
	}

	return destBundle.Name(), destManifestName, nil
}
