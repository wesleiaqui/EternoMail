#!/bin/bash
set -euo pipefail

output=${1:-"/tmp/eternomail-processes-$(date +%Y%m%d-%H%M%S).tsv"}
mkdir -p "$(dirname "$output")"
printf 'timestamp\tpid\tppid\tcpu_percent\trss_kib\tcommand\n' > "$output"
printf 'Sampling Eterno Mail and WebKit processes every second into %s (Ctrl-C to stop)\n' "$output"

while true; do
	ps -Ao pid=,ppid=,%cpu=,rss=,command= | awk -v timestamp="$(date +%Y-%m-%dT%H:%M:%S%z)" '
		tolower($0) ~ /eterno.?mail|postilaatikko|webkit\.webcontent|webkit\.gpu|webkit\.networking/ {
			command = substr($0, index($0, $5))
			printf "%s\t%s\t%s\t%s\t%s\t%s\n", timestamp, $1, $2, $3, $4, command
		}
	' >> "$output"
	sleep 1
done
