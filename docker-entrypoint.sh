#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
	data_dir=${MINIFORM_DATA_DIR:-/app/storage}
	logs_dir=${MINIFORM_LOGS_DIR:-"$data_dir/logs"}
	mkdir -p "$data_dir" "$logs_dir"
	if ! chown -R miniform:miniform "$data_dir" "$logs_dir" 2>/dev/null; then
		for directory in "$data_dir" "$logs_dir"; do
			if ! probe=$(su-exec miniform:miniform mktemp "$directory/.miniform-write-test.XXXXXX"); then
				echo "miniform: storage is not writable by uid 10001: $directory" >&2
				exit 1
			fi
			if ! su-exec miniform:miniform rm "$probe"; then
				echo "miniform: cannot remove storage write probe: $probe" >&2
				exit 1
			fi
		done
	fi
	exec su-exec miniform:miniform "$@"
fi

exec "$@"
