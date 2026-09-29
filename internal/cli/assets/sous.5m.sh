#!/bin/sh
# SwiftBar plugin: symlink into your SwiftBar plugin folder. Reads the sous
# cache every 5 minutes; never scans. The shell surface keeps the cache fresh.
exec "${SOUS_BIN:-$HOME/.local/bin/sous}" --menubar
