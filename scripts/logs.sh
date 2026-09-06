#!/bin/bash
# Open every job log in one live lnav view.
#
# The directory is the point, not a fixed file list: lnav watches it and
# picks up files that appear after it started. A hardcoded list goes stale
# the moment the next job is launched, which is exactly what happened the
# first two times.
#
# Every job in this project writes to /tmp/chesslogs/<name>.log.
#
# Keys: `/` search, `:filter-in <text>`, `:filter-out <text>`,
# `e` / `E` next and previous error, `TAB` switch view, `q` quit.
mkdir -p /tmp/chesslogs
exec lnav /tmp/chesslogs
