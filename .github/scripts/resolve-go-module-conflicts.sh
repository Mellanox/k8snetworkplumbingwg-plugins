#!/usr/bin/env bash
# 2026 NVIDIA CORPORATION & AFFILIATES
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Resolves the go.mod/go.sum/vendor conflicts left behind by an in-progress
# merge of upstream into this fork. Dependabot keeps this fork's dependencies
# ahead of upstream, so both sides regularly rewrite the same block of go.mod
# and the merge stops. Every other kind of conflict needs a human.

set -euo pipefail

conflicted=$(git diff --name-only --diff-filter=U)
unresolvable=$(grep -vE '^(go\.mod|go\.sum|vendor/)' <<<"$conflicted" || true)
if [ -n "$unresolvable" ]; then
	echo "::error::merge hit conflicts that must be resolved by hand:"
	echo "$unresolvable"
	exit 1
fi

is_conflicted() {
	grep -qxF "$1" <<<"$conflicted"
}

# True when $2 is the newer of the two versions.
is_newer() {
	[ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1)" = "$2" ]
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if is_conflicted go.mod; then
	git show :2:go.mod >"$tmp/ours.mod"
	git show :3:go.mod >"$tmp/theirs.mod"
	go mod edit -json "$tmp/ours.mod" >"$tmp/ours.json"
	go mod edit -json "$tmp/theirs.mod" >"$tmp/theirs.json"

	# Start from our side and pull in every upstream requirement that is newer
	# or new, so neither an upstream bump nor a Dependabot bump is lost.
	git checkout --ours -- go.mod
	while read -r path ours theirs; do
		if [ -z "$ours" ] || is_newer "$ours" "$theirs"; then
			go mod edit -require="$path@$theirs"
		fi
	done < <(jq -r --slurpfile ours "$tmp/ours.json" '
		($ours[0].Require // [] | map({ (.Path): .Version }) | add // {}) as $o
		| (.Require // [])[]
		| select($o[.Path] != .Version)
		| "\(.Path) \($o[.Path] // "") \(.Version)"' "$tmp/theirs.json")

	ours_go=$(jq -r '.Go // ""' "$tmp/ours.json")
	theirs_go=$(jq -r '.Go // ""' "$tmp/theirs.json")
	if [ -n "$theirs_go" ] && is_newer "$ours_go" "$theirs_go"; then
		go mod edit -go="$theirs_go"
	fi
fi

if is_conflicted go.sum; then
	# Union of both sides; `go mod tidy` drops whatever is no longer needed.
	git show :2:go.sum >"$tmp/ours.sum"
	git show :3:go.sum >"$tmp/theirs.sum"
	sort -u "$tmp/ours.sum" "$tmp/theirs.sum" >go.sum
fi

go mod tidy
go mod vendor
go build ./...

git add go.mod go.sum vendor
