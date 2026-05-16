#!/usr/bin/env bash
exec /usr/local/bin/civmctl hook job-completed --execute "$@"
