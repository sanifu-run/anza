#!/bin/sh
set -eu

if [ -z "${ANZA_CHAT_SOURCE:-}" ]; then
	printf '%s\n' 'ANZA_CHAT_SOURCE must point to the checked-out Chat repository.' >&2
	exit 2
fi
if [ ! -f "$ANZA_CHAT_SOURCE/go.mod" ]; then
	printf '%s\n' 'ANZA_CHAT_SOURCE does not contain a Chat Go module.' >&2
	exit 2
fi
if [ -n "$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1)" ]; then
	printf '%s\n' 'ANZA_CHAT_SOURCE must be a clean checkout of the reviewed Chat commit.' >&2
	exit 2
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ANZA_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
ANZA_CHAT_HEAD=$(git -C "$ANZA_CHAT_SOURCE" rev-parse HEAD)
ANZA_CHAT_TREE=$(git -C "$ANZA_CHAT_SOURCE" rev-parse 'HEAD^{tree}')
ANZA_CHAT_STATUS_BEFORE=$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
ANZA_HEAD=$(git -C "$ANZA_ROOT" rev-parse HEAD)
ANZA_TREE=$(git -C "$ANZA_ROOT" rev-parse 'HEAD^{tree}')
export ANZA_CHAT_SOURCE
GOCACHE=/private/tmp/anza-T10.1-go-cache
export GOCACHE

ONE_MINUTE_LOAD=$(uptime | sed 's/,//g' | awk '{print $(NF-2)}')
if ! awk -v load="$ONE_MINUTE_LOAD" 'BEGIN { exit !(load ~ /^[0-9]+([.][0-9]+)?$/) }'; then
	printf 'Unable to confirm a safe one-minute host load (%s); defer the CLI build.\n' "$ONE_MINUTE_LOAD" >&2
	exit 3
fi
if awk -v load="$ONE_MINUTE_LOAD" 'BEGIN { exit !(load > 10) }'; then
	printf 'Shared build host load is %s; defer the CLI build.\n' "$ONE_MINUTE_LOAD" >&2
	exit 3
fi

TASK_TMP=$(mktemp -d /private/tmp/anza-T10.1-contract.XXXXXX)
LEASE_HELD=0
LEASE_SHA=
cleanup() {
	if [ "$LEASE_HELD" -eq 1 ]; then
		CLAIM_REMOTE=/Users/Shared/mini-build-lease.git /Users/dndungu/.agents/skills/claim/scripts/claim.sh release R-build-lease "$LEASE_SHA"
	fi
	rm -rf "$TASK_TMP"
}
trap cleanup EXIT HUP INT TERM

CLAIM_OUTPUT=$(CLAIM_REMOTE=/Users/Shared/mini-build-lease.git /Users/dndungu/.agents/skills/claim/scripts/claim.sh claim R-build-lease --purpose "T10.1 Anza CLI subprocess build")
case "$CLAIM_OUTPUT" in
	"WON: R-build-lease "*) LEASE_SHA=${CLAIM_OUTPUT#WON: R-build-lease } ;;
	*)
		printf 'Shared build lease was not acquired: %s\n' "$CLAIM_OUTPUT" >&2
		exit 4
		;;
esac
case "$LEASE_SHA" in
	????????????????????????????????????????) ;;
	*) printf 'Build lease returned an invalid winner SHA: %s\n' "$LEASE_SHA" >&2; exit 4 ;;
esac
printf 'Build lease WON SHA: %s\n' "$LEASE_SHA"
LEASE_HELD=1
(cd "$ANZA_ROOT" && go build -o "$TASK_TMP/anza" ./cmd/anza)
CLAIM_REMOTE=/Users/Shared/mini-build-lease.git /Users/dndungu/.agents/skills/claim/scripts/claim.sh release R-build-lease "$LEASE_SHA"
LEASE_HELD=0
LEASE_SHA=
ANZA_BINARY=$TASK_TMP/anza
export ANZA_BINARY

printf 'Anza HEAD: %s (tree %s)\n' "$ANZA_HEAD" "$ANZA_TREE"
printf 'Chat HEAD: %s (tree %s)\n' "$ANZA_CHAT_HEAD" "$ANZA_CHAT_TREE"
printf '%s\n' 'Running the opt-in shared Chat subprocess contract with synthetic storage/provider only.'
TEST_STATUS=0
(cd "$ANZA_ROOT" && go test ./internal/acceptance -run '^(TestFreshJourney|TestExistingJourney|TestInterruptedJourney|TestBriefImportJourney|TestFeatureMismatchAndDisabledSetupAreActionable|TestSharedChatContract)$' -count=1) || TEST_STATUS=$?

ANZA_CHAT_STATUS_AFTER=$(git -C "$ANZA_CHAT_SOURCE" status --porcelain=v1 | shasum -a 256 | awk '{print $1}')
ANZA_CHAT_HEAD_AFTER=$(git -C "$ANZA_CHAT_SOURCE" rev-parse HEAD)
ANZA_CHAT_TREE_AFTER=$(git -C "$ANZA_CHAT_SOURCE" rev-parse 'HEAD^{tree}')
if [ "$ANZA_CHAT_STATUS_BEFORE" != "$ANZA_CHAT_STATUS_AFTER" ] || [ "$ANZA_CHAT_HEAD" != "$ANZA_CHAT_HEAD_AFTER" ] || [ "$ANZA_CHAT_TREE" != "$ANZA_CHAT_TREE_AFTER" ]; then
	printf '%s\n' 'Chat checkout changed during the contract run; inspect its current head, tree and status before relying on the recorded digests.' >&2
	exit 1
fi
printf 'Chat HEAD after run: %s (tree %s; status digest %s; unchanged)\n' "$ANZA_CHAT_HEAD_AFTER" "$ANZA_CHAT_TREE_AFTER" "$ANZA_CHAT_STATUS_AFTER"
exit "$TEST_STATUS"
