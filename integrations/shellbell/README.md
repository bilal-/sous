# Sous plugin for Shellbell

This directory is the Shellbell plugin package owned by the Sous repository.
Shellbell maintains the plugin registry and its headless plugin browser. The
registry points to a reviewed commit and this directory; plugin files are fetched
from that exact revision.

Install Sous on the computer first. Then, in a Shellbell version with plugin
support, run:

```sh
shellbell plugins
# Select Sous, review the integration, and choose Install.
shellbell plugins install sous
```

Installation registers the already-installed executable and icon with Shellbell.
It does not download Sous, run `sous setup`, or start a web server. Opening its
page on a paired Android phone starts `sous serve --json --port 0 --page-api 1` through
Shellbell's generic Pages host. Sign-in stays on the computer; no Sous-specific
wire protocol or native phone screen is required.

```sh
shellbell plugins info sous
shellbell plugins uninstall sous
```

Uninstall removes the Shellbell registration, leaving Sous and its data alone.
A missing executable is reported by the plugin browser. After moving Sous,
reinstall the integration to capture its new executable path. Custom page
registrations are preserved. `sous setup` does not opt users into Shellbell.

The manifest declares the required executable, supported platforms, page launch
arguments and optional PNG hash. It contains no install script. The icon is the
canonical `brand/icons/icon-flat-rounded-1024.png`; the Sous name and mark identify
this integration and remain subject to [the trademark notice](../../TRADEMARK.md).

The package and registry entry must be published before released users can fetch
this revision. Source checkouts can use Shellbell's explicit local preview
option with this directory. No publication is implied by the presence of these
files.

This package requires a Sous build supporting **page API 1**, including explicit
Load board and reconnect recovery. The launch argument gates this capability: an
older binary refuses startup with unsupported arguments instead of opening an
unusable board. Update Sous separately if that happens. No release version is
implied by this source package. Choose Load board on the phone for a first cache;
opening the page does not query providers. Loading is single-flight, bounded and
cancelled with the server. It never enters the automatic action retry queue.
