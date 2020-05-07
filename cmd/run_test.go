// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/open-policy-agent/opa/test/e2e"
)

func TestRunServerBase(t *testing.T) {
	params := newRunParams()
	params.rt = e2e.NewAPIServerTestParams()
	params.serverMode = true
	ctx, cancel := context.WithCancel(context.Background())

	rt := initRuntime(ctx, params, nil)

	testRuntime := e2e.WrapRuntime(ctx, cancel, rt)

	go startRuntime(ctx, rt, true)

	err := testRuntime.WaitForServer()
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	err = testRuntime.UploadData(bytes.NewBufferString(`{"x": 1}`))
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	resp := struct {
		Result int `json:"result"`
	}{}
	err = testRuntime.GetDataWithInputTyped("x", nil, &resp)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	if resp.Result != 1 {
		t.Fatalf("Expected x to be 1, got %v", resp)
	}
}
