#!/usr/bin/env bash
# Copyright 2019 The Wire Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# https://coderwall.com/p/fkfaqq/safer-bash-scripts-with-set-euxo-pipefail
set -euxo pipefail

if [[ $# -gt 0 ]]; then
  echo "usage: runtests.sh" 1>&2
  exit 64
fi

result=0

# Run Go tests. Only do coverage for the Linux build
# because it is slow, and Coveralls will only save the last one anyway.
if [[ "$TRAVIS_OS_NAME" == "linux" ]]; then
  go test -race -coverpkg=./... -coverprofile=coverage.out ./... || result=1
  if [ -f coverage.out ]; then
    goveralls -coverprofile=coverage.out -service=travis-ci
  fi
  # Ensure that the code has no extra dependencies (including transitive
  # dependencies) that we're not already aware of by comparing with
  # ./internal/alldeps
  #
  # Whenever project dependencies change, rerun ./internal/listdeps.sh
  ./internal/listdeps.sh | diff ./internal/alldeps - || {
    echo "FAIL: dependencies changed; compare listdeps.sh output with alldeps" && result=1
  }
else
  go test -race ./... || result=1
fi

exit $result
