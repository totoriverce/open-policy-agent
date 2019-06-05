// Copyright 2019 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/runtime"
	"github.com/open-policy-agent/opa/util"
)

const (
	defaultAddr = "127.0.0.1:8181" // default listening address for server
)

// NewAPIServerTestParams creates a new set of runtime.Params with enough
// default values filled in to start the server. Options can/should
// be customized for the test case.
func NewAPIServerTestParams() runtime.Params {
	params := runtime.NewParams()

	// Add in some defaults
	params.Addrs = &[]string{defaultAddr}

	params.Logging = runtime.LoggingConfig{
		Level:  "debug",
		Format: "json-pretty",
	}

	return params
}

func apiServerURL(params runtime.Params) (string, error) {

	addr := (*params.Addrs)[0] // probably fine.. if not then test blows up ?

	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	if !strings.Contains(addr, "://") {
		scheme := "http://"
		if params.Certificate != nil {
			scheme = "https://"
		}
		addr = scheme + addr
	}

	parsed, err := url.Parse(addr)
	if err != nil {
		return "", err
	}

	return parsed.String(), nil
}

// TestRuntime holds metadata and provides helper methods
// to interact with the runtime being tested.
type TestRuntime struct {
	URL     string
	Params  runtime.Params
	Runtime *runtime.Runtime
	Ctx     context.Context
	Cancel  context.CancelFunc
	Client  *http.Client
}

// NewTestRuntime returns a new TestRuntime which
func NewTestRuntime(params runtime.Params) (*TestRuntime, error) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	rt, err := runtime.NewRuntime(ctx, params)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("Unable to create new runtime: %s", err)
	}

	url, err := apiServerURL(params)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("Unable to determine runtime URL: %s", err)
	}

	return &TestRuntime{
		URL:     url,
		Params:  params,
		Runtime: rt,
		Ctx:     ctx,
		Cancel:  cancel,
		Client:  &http.Client{},
	}, nil
}

// RunAPIServerTests will start the OPA runtime serving with a given
// configuration. This is essentially a wrapper for `m.Run()` that
// handles starting and stopping the local API server. The return
// value is what should be used as the code in `os.Exit` in the
// `TestMain` function.
func (t *TestRuntime) RunAPIServerTests(m *testing.M) int {
	// Start serving API requests in the background
	done := make(chan error)
	go func() {
		err := t.Runtime.Serve(t.Ctx)
		done <- err
	}()

	// Actually run the unit tests/benchmarks
	errc := m.Run()

	// Wait for the API server to stop
	t.Cancel()
	err := <-done

	if err != nil && errc == 0 {
		// even if the tests passed return an error code if
		// the server encountered an error
		errc = 1
	}

	return errc
}

// UploadPolicy will upload the given policy to the runtime via the v1 policy API
func (t *TestRuntime) UploadPolicy(name string, policy io.Reader) error {
	req, err := http.NewRequest("PUT", t.URL+"/v1/policies/"+name, policy)
	if err != nil {
		return fmt.Errorf("Unexpected error creating request: %s", err)
	}
	resp, err := t.Client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to PUT the test policy: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected response: %d %s", resp.StatusCode, resp.Status)
	}
	return nil
}

// UploadData will upload the given data to the runtime via the v1 data API
func (t *TestRuntime) UploadData(data io.Reader) error {
	client := &http.Client{}
	req, err := http.NewRequest("PUT", t.URL+"/v1/data", data)
	if err != nil {
		return fmt.Errorf("Unexpected error creating request: %s", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to PUT data: %s", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Unexpected response: %d %s", resp.StatusCode, resp.Status)
	}
	return nil
}

// GetDataWithInput will use the v1 data API and POST with the given input. The returned
// value is the full response body.
func (t *TestRuntime) GetDataWithInput(path string, input interface{}) ([]byte, error) {
	inputPayload := util.MustMarshalJSON(map[string]interface{}{
		"input": input,
	})

	path = strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(path, "data") {
		path = "data/" + path
	}

	resp, err := http.Post(t.URL+"/v1/"+path, "application/json", bytes.NewReader(inputPayload))
	if err != nil {
		return nil, fmt.Errorf("Unexpected error: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected response status: %d %s", resp.StatusCode, resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Unexpected error reading response body: %s", err)
	}

	return body, nil
}
