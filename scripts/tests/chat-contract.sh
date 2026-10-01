#!/bin/sh
set -eu

if [ -z "${ANZA_CHAT_SOURCE:-}" ]; then
	printf '%s\n' 'ANZA_CHAT_SOURCE must point to the reviewed Chat Git checkout.' >&2
	exit 2
fi
if [ ! -f "$ANZA_CHAT_SOURCE/go.mod" ] || ! git -C "$ANZA_CHAT_SOURCE" rev-parse --verify HEAD >/dev/null 2>&1; then
	printf '%s\n' 'ANZA_CHAT_SOURCE must point to a Chat Git checkout with a Go module.' >&2
	exit 2
fi

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ANZA_ROOT=$(CDPATH='' cd -- "$SCRIPT_DIR/../.." && pwd)
ANZA_CHAT_HEAD=$(git -C "$ANZA_CHAT_SOURCE" rev-parse HEAD)
ANZA_CHAT_TREE=$(git -C "$ANZA_CHAT_SOURCE" rev-parse 'HEAD^{tree}')
ANZA_CHAT_STATUS_BEFORE=$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
ANZA_HEAD=$(git -C "$ANZA_ROOT" rev-parse HEAD)
ANZA_TREE=$(git -C "$ANZA_ROOT" rev-parse 'HEAD^{tree}')
ANZA_STATUS_BEFORE=$(git -C "$ANZA_ROOT" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
export ANZA_CHAT_SOURCE
GOCACHE=/private/tmp/anza-T10.1-go-cache
export GOCACHE

printf 'Anza HEAD: %s (tree %s; status %s)\n' "$ANZA_HEAD" "$ANZA_TREE" "$ANZA_STATUS_BEFORE"
printf 'Chat HEAD: %s (tree %s; status %s)\n' "$ANZA_CHAT_HEAD" "$ANZA_CHAT_TREE" "$ANZA_CHAT_STATUS_BEFORE"
printf '%s\n' 'Running the socketless real-router contract with synthetic storage/provider only.'
TEST_STATUS=0
(cd "$ANZA_ROOT" && go test ./internal/acceptance -run '^TestSharedChatProtocolContract$' -count=1 -v) || TEST_STATUS=$?
printf '%s\n' 'Running the negative control against a deliberately renamed capabilities route in the disposable Chat archive.'
(cd "$ANZA_ROOT" && ANZA_SOCKETLESS_NEGATIVE_CONTROL=capabilities-route go test ./internal/acceptance -run '^TestSharedChatProtocolContract$' -count=1 -v)
CLI_STATUS=0
if [ "${ANZA_RUN_SOCKET_CLI:-}" = "1" ]; then
	if [ -z "${ANZA_BINARY:-}" ] || [ ! -x "$ANZA_BINARY" ]; then
		printf '%s\n' 'ANZA_RUN_SOCKET_CLI=1 requires ANZA_BINARY to point to a previously built Anza CLI.' >&2
		exit 2
	fi
	printf '%s\n' 'Running the opt-in interactive CLI journeys; these require loopback listener access.'
	(cd "$ANZA_ROOT" && go test ./internal/acceptance -run '^(TestFreshJourney|TestExistingJourney|TestInterruptedJourney|TestBriefImportJourney|TestFeatureMismatchAndDisabledSetupAreActionable|TestSharedChatContract)$' -count=1) || CLI_STATUS=$?
else
	printf '%s\n' 'Interactive CLI journeys remain opt-in; set ANZA_RUN_SOCKET_CLI=1 and ANZA_BINARY to run them.'
fi

ANZA_CHAT_STATUS_AFTER=$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
ANZA_CHAT_HEAD_AFTER=$(git -C "$ANZA_CHAT_SOURCE" rev-parse HEAD)
ANZA_CHAT_TREE_AFTER=$(git -C "$ANZA_CHAT_SOURCE" rev-parse 'HEAD^{tree}')
ANZA_STATUS_AFTER=$(git -C "$ANZA_ROOT" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
if [ "$ANZA_CHAT_STATUS_BEFORE" != "$ANZA_CHAT_STATUS_AFTER" ] || [ "$ANZA_CHAT_HEAD" != "$ANZA_CHAT_HEAD_AFTER" ] || [ "$ANZA_CHAT_TREE" != "$ANZA_CHAT_TREE_AFTER" ]; then
	printf '%s\n' 'Chat checkout changed during the contract run; inspect its current revision and status before relying on the recorded digests.' >&2
	exit 1
fi
if [ "$ANZA_HEAD" != "$(git -C "$ANZA_ROOT" rev-parse HEAD)" ] || [ "$ANZA_TREE" != "$(git -C "$ANZA_ROOT" rev-parse 'HEAD^{tree}')" ] || [ "$ANZA_STATUS_BEFORE" != "$ANZA_STATUS_AFTER" ]; then
	printf '%s\n' 'Anza checkout changed during the contract run; inspect its revision and status before relying on the recorded digests.' >&2
	exit 1
fi
printf 'Chat unchanged after run: HEAD %s (tree %s; status %s)\n' "$ANZA_CHAT_HEAD_AFTER" "$ANZA_CHAT_TREE_AFTER" "$ANZA_CHAT_STATUS_AFTER"
printf 'Anza unchanged after run: HEAD %s (tree %s; status %s)\n' "$ANZA_HEAD" "$ANZA_TREE" "$ANZA_STATUS_AFTER"
if [ "$TEST_STATUS" -ne 0 ]; then
	exit "$TEST_STATUS"
fi
exit "$CLI_STATUS"
