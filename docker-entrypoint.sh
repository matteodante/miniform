#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
	trim_space() {
		printf '%s\n' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
	}

	normalize_storage_path() {
		path=$1
		case "$path" in
		/*) ;;
		*) path=/app/$path ;;
		esac
		while [ "$path" != "/" ] && [ "${path%/}" != "$path" ]; do
			path=${path%/}
		done
		case "$path/" in
		*//*|*/./*|*/../*)
			echo "miniform: storage path must be absolute and normalized: $1" >&2
			return 1
			;;
		esac
		printf '%s\n' "$path"
	}

	refuse_symlink_components() {
		component=$1
		while [ "$component" != "/" ]; do
			if [ -L "$component" ]; then
				echo "miniform: refusing symbolic link in storage path: $component" >&2
				return 1
			fi
			component=$(dirname "$component")
		done
	}

	environment=$(trim_space "${MINIFORM_ENV:-}")
	environment=${environment:-development}
	data_dir=$(trim_space "${MINIFORM_DATA_DIR:-}")
	data_dir=$(normalize_storage_path "${data_dir:-/app/storage}")
	logs_dir=$(trim_space "${MINIFORM_LOGS_DIR:-}")
	logs_dir=${logs_dir:-"$data_dir/logs"}
	logs_dir=$(normalize_storage_path "$logs_dir")
	database_path=$(trim_space "${MINIFORM_DATABASE_PATH:-}")
	if [ -z "$database_path" ]; then
		database_filename=$(trim_space "${MINIFORM_DATABASE_FILENAME:-}")
		database_filename=${database_filename:-miniform.db}
		case "$database_filename" in
		.|..|/*|*/*)
			echo "miniform: MINIFORM_DATABASE_FILENAME must be a filename; use MINIFORM_DATABASE_PATH for a path" >&2
			exit 1
			;;
		esac
		case "$database_filename" in
			*.*)
				database_base=${database_filename%.*}
				database_extension=.${database_filename##*.}
				;;
			*)
				database_base=$database_filename
				database_extension=.db
				;;
		esac
		database_path=$data_dir/$database_base.$environment$database_extension
	fi
	database_path=$(normalize_storage_path "$database_path")
	database_dir=$(dirname "$database_path")

	for storage_path in "$data_dir" "$logs_dir" "$database_path"; do
		refuse_symlink_components "$storage_path"
	done
	mkdir -p "$data_dir" "$logs_dir" "$database_dir"
	for storage_path in "$data_dir" "$logs_dir" "$database_path"; do
		refuse_symlink_components "$storage_path"
	done
	chown miniform:miniform "$data_dir" "$logs_dir" "$database_dir" 2>/dev/null || true
	if ! find "$logs_dir" -xdev -exec chown -h miniform:miniform {} \;; then
		echo "miniform: cannot assign restored logs to uid 10001: $logs_dir" >&2
		exit 1
	fi
	for storage_tree in "$data_dir/uploads" "$data_dir/.upload-staging" "$data_dir/.upload-deletions"; do
		if [ -L "$storage_tree" ]; then
			echo "miniform: refusing symbolic link for upload storage: $storage_tree" >&2
			exit 1
		fi
		if [ -e "$storage_tree" ] && [ ! -d "$storage_tree" ]; then
			echo "miniform: upload storage is not a directory: $storage_tree" >&2
			exit 1
		fi
		if [ -d "$storage_tree" ] && ! find "$storage_tree" -xdev -exec chown -h miniform:miniform {} \;; then
			echo "miniform: cannot assign restored upload storage to uid 10001: $storage_tree" >&2
			exit 1
		fi
	done
	for database_file in "$database_path" "$database_path-wal" "$database_path-shm"; do
		if [ -L "$database_file" ]; then
			echo "miniform: refusing symbolic link for SQLite storage: $database_file" >&2
			exit 1
		fi
		if [ -e "$database_file" ]; then
			chown miniform:miniform "$database_file" 2>/dev/null || true
			if ! su-exec miniform:miniform test -r "$database_file"; then
				echo "miniform: SQLite storage is not readable by uid 10001: $database_file" >&2
				exit 1
			fi
			if ! su-exec miniform:miniform test -w "$database_file"; then
				echo "miniform: SQLite storage is not writable by uid 10001: $database_file" >&2
				exit 1
			fi
		fi
	done
	for directory in "$data_dir" "$logs_dir" "$database_dir"; do
		if ! probe=$(su-exec miniform:miniform mktemp "$directory/.miniform-write-test.XXXXXX"); then
			echo "miniform: storage is not writable by uid 10001: $directory" >&2
			exit 1
		fi
		if ! su-exec miniform:miniform rm "$probe"; then
			echo "miniform: cannot remove storage write probe: $probe" >&2
			exit 1
		fi
	done
	exec su-exec miniform:miniform "$@"
fi

exec "$@"
