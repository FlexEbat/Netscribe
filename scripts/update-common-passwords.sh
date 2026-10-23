#!/usr/bin/env bash
# Rebuilds internal/auth/common_passwords.txt from the SecLists leak lists (MIT licence).
#
# Only passwords of 12 or more characters are kept, because the service accepts nothing
# shorter. Entries are lower case, printable ASCII, most common first, at most 20000.
# Run it when you want a fresher list, review the diff, then commit the file.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/internal/auth/common_passwords.txt"
base="https://raw.githubusercontent.com/danielmiessler/SecLists/master/Passwords/Common-Credentials"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/pwdb.txt" "$base/Pwdb_top-1000000.txt"
curl -fsSL -o "$tmp/ncsc.txt" "$base/100k-most-used-passwords-NCSC.txt"

{
  echo "# Passwords of 12 or more characters taken from public leaks, lower case, most common first."
  echo "# Source: SecLists Passwords/Common-Credentials (Pwdb_top-1000000 and 100k-most-used-passwords-NCSC), MIT licence, (c) 2018 Daniel Miessler."
  echo "# Netscribe-specific entries come first. Rebuild with scripts/update-common-passwords.sh."
  printf '%s\n' netscribe12345 netscribe1234 netscribepassword netscribe-admin netscribe-password \
    changemechangeme changeme12345 adminadminadmin
  cat "$tmp/pwdb.txt" "$tmp/ncsc.txt" |
    tr -d '\r' | awk 'length($0)>=12 && length($0)<=64' | tr 'A-Z' 'a-z' |
    grep -P '^[\x21-\x7e]+$' | awk '!seen[$0]++' | awk 'NR<=20000'
} | awk '!seen[$0]++' >"$out"

echo "wrote $(wc -l <"$out") lines to $out"
