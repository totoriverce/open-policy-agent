package bundle

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	opaBundle "github.com/open-policy-agent/opa/bundle"
)

func TestLoadBundleFromDisk_Legacy(t *testing.T) {
	var err error

	tempDir := t.TempDir()

	bundle := opaBundle.Bundle{
		Manifest: opaBundle.Manifest{
			Revision: "rev1",
		},
		Data: map[string]interface{}{
			"foo": "bar",
		},
	}

	var buf bytes.Buffer
	err = opaBundle.NewWriter(&buf).Write(bundle)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	bundlePath := filepath.Join(tempDir, "bundle.tar.gz")
	f, err := os.Create(bundlePath)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	_, err = io.Copy(f, &buf)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	loadedBundle, err := LoadBundleFromDisk(tempDir, &LoadOptions{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if !loadedBundle.Equal(bundle) {
		t.Fatalf("unexpected bundle: %#v", loadedBundle)
	}
}

func TestLoadBundleFromDisk_BundlePackage(t *testing.T) {
	var err error

	tempDir := t.TempDir()

	bundle := opaBundle.Bundle{
		Manifest: opaBundle.Manifest{
			Revision: "rev1",
		},
		Data: map[string]interface{}{
			"foo": "bar",
		},
	}

	var buf bytes.Buffer
	err = opaBundle.NewWriter(&buf).Write(bundle)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	err = SaveBundleToDisk(tempDir, &buf, &SaveOptions{Etag: "123"})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	loadedBundle, err := LoadBundleFromDisk(tempDir, &LoadOptions{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if !loadedBundle.Equal(bundle) {
		t.Fatalf("unexpected bundle: %#v", loadedBundle)
	}

	if loadedBundle.Etag != "123" {
		t.Fatalf("unexpected etag: %s", bundle.Etag)
	}
}

func TestSaveBundleToDisk(t *testing.T) {
	var err error

	tempDir := t.TempDir()

	bundle := opaBundle.Bundle{
		Manifest: opaBundle.Manifest{
			Revision: "rev1",
		},
		Data: map[string]interface{}{
			"foo": "bar",
		},
	}

	var buf bytes.Buffer
	err = opaBundle.NewWriter(&buf).Write(bundle)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	err = SaveBundleToDisk(tempDir, &buf, &SaveOptions{Etag: "123"})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	loadedBundle, err := LoadBundleFromDisk(tempDir, &LoadOptions{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if !loadedBundle.Equal(bundle) {
		t.Fatalf("unexpected bundle: %#v", loadedBundle)
	}

	if loadedBundle.Etag != "123" {
		t.Fatalf("unexpected etag: %s", loadedBundle.Etag)
	}
}

func TestSaveBundleToDisk_Overwrite(t *testing.T) {
	var err error

	tempDir := t.TempDir()

	bundle1 := opaBundle.Bundle{
		Manifest: opaBundle.Manifest{
			Revision: "rev1",
		},
		Data: map[string]interface{}{
			"foo": "bar",
		},
	}
	bundle2 := opaBundle.Bundle{
		Manifest: opaBundle.Manifest{
			Revision: "rev2",
		},
		Data: map[string]interface{}{
			"foo": "baz",
		},
	}

	var buf1 bytes.Buffer
	err = opaBundle.NewWriter(&buf1).Write(bundle1)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	var buf2 bytes.Buffer
	err = opaBundle.NewWriter(&buf2).Write(bundle2)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	bundle1Etag := "123"
	bundle2Etag := "456"

	// write the first version of the bundle to disk
	err = SaveBundleToDisk(tempDir, &buf1, &SaveOptions{Etag: bundle1Etag})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	// load the bundle and validate it
	loadedBundle, err := LoadBundleFromDisk(tempDir, &LoadOptions{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if !loadedBundle.Equal(bundle1) {
		t.Fatalf("unexpected bundle: %#v", loadedBundle)
	}

	if loadedBundle.Etag != bundle1Etag {
		t.Fatalf("unexpected etag: %s", loadedBundle.Etag)
	}

	// overwrite the bundle
	err = SaveBundleToDisk(tempDir, &buf2, &SaveOptions{Etag: bundle2Etag})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	// load the new bundle and validate it
	loadedBundle, err = LoadBundleFromDisk(tempDir, &LoadOptions{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if !loadedBundle.Equal(bundle2) {
		t.Fatalf("unexpected bundle: %#v", loadedBundle)
	}

	if loadedBundle.Etag != bundle2Etag {
		t.Fatalf("unexpected etag: %s", loadedBundle.Etag)
	}
}

func TestSaveBundleToDisk_NewPath(t *testing.T) {
	var err error

	tempDir := t.TempDir()

	bundle := opaBundle.Bundle{
		Manifest: opaBundle.Manifest{
			Revision: "rev1",
		},
		Data: map[string]interface{}{
			"foo": "bar",
		},
	}

	var buf bytes.Buffer
	err = opaBundle.NewWriter(&buf).Write(bundle)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	bundlePath := filepath.Join(tempDir, "foo", "bar", "example1")

	err = SaveBundleToDisk(bundlePath, &buf, &SaveOptions{Etag: "123"})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	loadedBundle, err := LoadBundleFromDisk(bundlePath, &LoadOptions{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if !loadedBundle.Equal(bundle) {
		t.Fatalf("unexpected bundle: %#v", loadedBundle)
	}

	if loadedBundle.Etag != "123" {
		t.Fatalf("unexpected etag: %s", bundle.Etag)
	}
}

func TestSaveBundleToDisk_Nil(t *testing.T) {
	var err error
	srcDir := t.TempDir()

	err = SaveBundleToDisk(srcDir, nil, &SaveOptions{})
	if err == nil {
		t.Fatal("expected error but got nil")
	}

	expErrMsg := "no raw bundle bytes to persist to disk"
	if err.Error() != expErrMsg {
		t.Fatalf("expected error: %v but got: %v", expErrMsg, err)
	}
}