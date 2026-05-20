#!/bin/sh
set -eu
# shellcheck source=common.sh
. "$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)/common.sh"
dotvault_dispatch_hook session-end
