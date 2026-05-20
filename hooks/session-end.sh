#!/bin/sh
set -eu
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"
dotvault_dispatch_hook session-end
