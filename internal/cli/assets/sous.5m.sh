#!/bin/sh
# SwiftBar plugin: link it into your SwiftBar plugin folder. It reads the
# saved board every 5 minutes and never scans; new shells keep it fresh.
# sous setup writes where sous is installed below; SOUS_BIN overrides it.
exec "${SOUS_BIN:-$HOME/.local/bin/sous}" --menubar
