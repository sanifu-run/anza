#!/bin/sh
set -eu

ANZA_CHAT_FIXTURE_COMMIT=${ANZA_CHAT_FIXTURE_COMMIT:-869a89d8e121f97c03647b0b61ee712d8019949d}
if [ -z "${ANZA_CHAT_SOURCE:-}" ]; then
	printf '%s\n' 'ANZA_CHAT_SOURCE must point to the reviewed Chat Git checkout.' >&2
	exit 2
fi
if [ ! -f "$ANZA_CHAT_SOURCE/go.mod" ] || ! git -C "$ANZA_CHAT_SOURCE" rev-parse --verify HEAD >/dev/null 2>&1; then
	printf '%s\n' 'ANZA_CHAT_SOURCE must point to a Chat Git checkout with a Go module.' >&2
	exit 2
fi
if ! git -C "$ANZA_CHAT_SOURCE" cat-file -e "$ANZA_CHAT_FIXTURE_COMMIT^{commit}" 2>/dev/null; then
	printf 'Chat checkout does not contain requested reviewed main commit %s.\n' "$ANZA_CHAT_FIXTURE_COMMIT" >&2
	exit 2
fi

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ANZA_ROOT=$(CDPATH='' cd -- "$SCRIPT_DIR/../.." && pwd)
ANZA_CHAT_SOURCE_HEAD=$(git -C "$ANZA_CHAT_SOURCE" rev-parse HEAD)
ANZA_CHAT_SOURCE_TREE=$(git -C "$ANZA_CHAT_SOURCE" rev-parse 'HEAD^{tree}')
ANZA_CHAT_HEAD=$ANZA_CHAT_FIXTURE_COMMIT
ANZA_CHAT_TREE=$(git -C "$ANZA_CHAT_SOURCE" rev-parse "$ANZA_CHAT_FIXTURE_COMMIT^{tree}")
ANZA_CHAT_STATUS_BEFORE=$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
ANZA_HEAD=$(git -C "$ANZA_ROOT" rev-parse HEAD)
ANZA_TREE=$(git -C "$ANZA_ROOT" rev-parse 'HEAD^{tree}')
ANZA_STATUS_BEFORE=$(git -C "$ANZA_ROOT" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')

ANZA_TASK_TMP_DIR=${ANZA_TASK_TMP_DIR:-/Volumes/BuildOffload/tmp/anza-t101-20261001}
case "$ANZA_TASK_TMP_DIR" in
	/Volumes/BuildOffload/*) ;;
	*) printf '%s\n' 'ANZA_TASK_TMP_DIR must be on /Volumes/BuildOffload.' >&2; exit 2 ;;
esac
mkdir -p "$ANZA_TASK_TMP_DIR"
case "${TMPDIR:-}" in
	/Volumes/BuildOffload/*) ;;
	*) TMPDIR="$ANZA_TASK_TMP_DIR/tmp" ;;
esac
mkdir -p "$TMPDIR"
export TMPDIR
case "${GOCACHE:-}" in
	/Volumes/BuildOffload/*) ;;
	*) GOCACHE="$ANZA_TASK_TMP_DIR/gocache" ;;
esac
mkdir -p "$GOCACHE"
export GOCACHE
ANZA_BINARY="$ANZA_TASK_TMP_DIR/anza"
export ANZA_BINARY ANZA_CHAT_SOURCE ANZA_CHAT_FIXTURE_COMMIT
unset ANZA_LIVE_EVAL ANZA_ENABLE_LIVE_EVAL ANZA_RUN_LIVE_EVAL ANZA_LIVE_PROVIDER ANZA_ENABLE_LIVE_PROVIDER 2>/dev/null || true

verify_checkout_unchanged() {
	status=$?
	trap - EXIT
	ANZA_CHAT_STATUS_AFTER=$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
	ANZA_CHAT_SOURCE_HEAD_AFTER=$(git -C "$ANZA_CHAT_SOURCE" rev-parse HEAD)
	ANZA_CHAT_SOURCE_TREE_AFTER=$(git -C "$ANZA_CHAT_SOURCE" rev-parse 'HEAD^{tree}')
	ANZA_STATUS_AFTER=$(git -C "$ANZA_ROOT" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
	if [ "$ANZA_CHAT_STATUS_BEFORE" != "$ANZA_CHAT_STATUS_AFTER" ] || [ "$ANZA_CHAT_SOURCE_HEAD" != "$ANZA_CHAT_SOURCE_HEAD_AFTER" ] || [ "$ANZA_CHAT_SOURCE_TREE" != "$ANZA_CHAT_SOURCE_TREE_AFTER" ]; then
		printf '%s\n' 'Chat checkout changed during the contract run; inspect its current revision and status before relying on the recorded digests.' >&2
		status=1
	fi
	if [ "$ANZA_HEAD" != "$(git -C "$ANZA_ROOT" rev-parse HEAD)" ] || [ "$ANZA_TREE" != "$(git -C "$ANZA_ROOT" rev-parse 'HEAD^{tree}')" ] || [ "$ANZA_STATUS_BEFORE" != "$ANZA_STATUS_AFTER" ]; then
		printf '%s\n' 'Anza checkout changed during the contract run; inspect its revision and status before relying on the recorded digests.' >&2
		status=1
	fi
	if [ "$status" -eq 0 ]; then
		printf 'Chat fixture archived commit %s (tree %s); source checkout unchanged at %s (tree %s; status %s)\n' "$ANZA_CHAT_HEAD" "$ANZA_CHAT_TREE" "$ANZA_CHAT_SOURCE_HEAD_AFTER" "$ANZA_CHAT_SOURCE_TREE_AFTER" "$ANZA_CHAT_STATUS_AFTER"
		printf 'Anza unchanged after run: HEAD %s (tree %s; status %s)\n' "$ANZA_HEAD" "$ANZA_TREE" "$ANZA_STATUS_AFTER"
	fi
	exit "$status"
}
trap verify_checkout_unchanged EXIT

printf 'Anza HEAD: %s (tree %s; status %s)\n' "$ANZA_HEAD" "$ANZA_TREE" "$ANZA_STATUS_BEFORE"
printf 'Chat fixture commit: %s (tree %s); source checkout HEAD: %s (tree %s; status %s)\n' "$ANZA_CHAT_HEAD" "$ANZA_CHAT_TREE" "$ANZA_CHAT_SOURCE_HEAD" "$ANZA_CHAT_SOURCE_TREE" "$ANZA_CHAT_STATUS_BEFORE"
printf 'External task temp: %s; GOCACHE: %s\n' "$TMPDIR" "$GOCACHE"

printf '%s\n' 'Building the actual Anza CLI for loopback journey tests.'
(cd "$ANZA_ROOT" && go build -o "$ANZA_BINARY" ./cmd/anza)

printf '%s\n' 'Running the socketless real-router contract with synthetic storage/provider only.'
(cd "$ANZA_ROOT" && go test ./internal/acceptance -run '^TestSharedChatProtocolContract$' -count=1 -v)

printf '%s\n' 'Running the negative control against a deliberately renamed capabilities route in the disposable reviewed Chat archive.'
(cd "$ANZA_ROOT" && ANZA_SOCKETLESS_NEGATIVE_CONTROL=capabilities-route go test ./internal/acceptance -run '^TestSharedChatProtocolContract$' -count=1 -v)

printf '%s\n' 'Running actual interactive Anza CLI journeys against the loopback HTTPS proxy and archived Chat router.'
(cd "$ANZA_ROOT" && go test ./internal/acceptance -run '^(TestCLIPrivateStateIsolationPreservesHome|TestFreshJourney|TestExistingJourney|TestInterruptedJourney|TestBriefImportJourney|TestFeatureMismatchAndDisabledSetupAreActionable|TestSharedChatContract)$' -count=1 -v)
