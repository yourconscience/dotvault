#!/bin/sh
set -eu

dotvault_hook_entry_dir=${0%/*}
if [ "$dotvault_hook_entry_dir" = "$0" ]; then
  dotvault_hook_entry_dir=.
elif [ -z "$dotvault_hook_entry_dir" ]; then
  dotvault_hook_entry_dir=/
fi
case $dotvault_hook_entry_dir in
  /*|./*|../*|.) ;;
  *) dotvault_hook_entry_dir=./$dotvault_hook_entry_dir ;;
esac

. "$(CDPATH= cd "$dotvault_hook_entry_dir" && pwd)/common.sh"
unset dotvault_hook_entry_dir

dotvault_dispatch_hook session-start
