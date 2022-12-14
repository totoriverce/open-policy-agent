#!/usr/bin/env bash
set -eo pipefail
OPA_EXEC="$1"
TARGET="$2"

opa() {
    local args="$*"
    echo "::group::$args"
    $OPA_EXEC $args
    echo "::endgroup::"
}

assert_contains() {
    local expected="$1"
    local actual="$2"
    if [[ "$actual" != *"$expected"* ]]; then
        exit 1
    fi
}

opa version
opa eval -t $TARGET 'time.now_ns()'
opa eval --format pretty --bundle test/cli/smoke/golden-bundle.tar.gz --input test/cli/smoke/input.json data.test.result --fail
opa exec --bundle test/cli/smoke/golden-bundle.tar.gz --decision test/result test/cli/smoke/input.json
opa build --output o0.tar.gz test/cli/smoke/test.rego
opa eval --format pretty --bundle o0.tar.gz --input test/cli/smoke/input.json data.test.result --fail
assert_contains('/test/cli/smoke/test.rego' $(opa inspect o0.tar.gz)) # check that the bundle contains the expected file with correct path
opa build --optimize 1 --output o1.tar.gz test/cli/smoke/test.rego
opa eval --format pretty --bundle o1.tar.gz --input test/cli/smoke/input.json data.test.result --fail
opa build --optimize 2 --output o2.tar.gz test/cli/smoke/test.rego
opa eval --format pretty --bundle o2.tar.gz --input test/cli/smoke/input.json data.test.result --fail