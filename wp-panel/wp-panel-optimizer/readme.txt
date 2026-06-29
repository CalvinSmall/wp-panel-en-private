=== WP Panel Optimizer ===
Contributors: naibabiji
Requires at least: 5.0
Tested up to: 7.0
Requires PHP: 8.1
Stable tag: 1.1.5
License: GPL-2.0+
License URI: https://www.gnu.org/licenses/gpl-2.0.html

Works together with the WP Panel server management panel to manage FastCGI caching, disable update checks, disable file editing, and other optimizations from within the WordPress admin — with two-way sync to the panel settings.

== Description ==

WP Panel Optimizer is the companion plugin for [WP Panel](https://github.com/naibabiji/wp-panel), syncing optimization settings in real time with the server-side panel via its API.

Author: [naibabiji](https://blog.naibabiji.com) | Plugin: [GitHub](https://github.com/naibabiji/wp-panel)

= Features =

* **FastCGI Cache Management**: Enable or disable Nginx FastCGI full-page caching from the WordPress admin, and set cache TTL
* **Cache Preloading**: Manually or automatically (after clearing cache) visit all public pages so Nginx generates FastCGI cache files in the background
* **Disable Update Checks**: Completely block WordPress update detection (no red badges or notifications in the dashboard; the "Check for Updates" button has no effect). To update, first turn off this toggle, then check
* **Disable File Editing**: Write the DISALLOW_FILE_EDIT constant to wp-config.php
* **Admin Bar Quick Clear**: One-click Nginx cache clear from the WordPress admin bar
* **Auto-Clear Cache**: Automatically clears cache on publish / update / delete
* **Two-Way Panel Sync**: Settings are pushed to the panel after changes, and the latest panel state is pulled automatically

= Requirements =

* WP Panel v1.0.0-beta2+ already installed
* The plugin is installed automatically by the panel (Site Detail → WordPress Optimization → Install Companion Plugin); no manual upload required

== Installation ==

1. Go to the site detail page in WP Panel
2. In the "WordPress Optimization" card, check the features you want to enable
3. Click "Install Companion Plugin" — the panel auto-deploys the plugin to the site's wp-content/plugins/
4. Activate the plugin in the WordPress admin, or let the panel auto-activate it

After installation, the panel writes a config file (containing the panel URL and API Key) to /var/wp-panel/site-secrets/<domain>/wp-panel-config.json, outside the web root. No manual credential entry needed.

== Changelog ==

= 1.1.5 =
* Added a "Clear Nginx Cache" button on the plugin settings page for manual cache clearing from mobile admin

= 1.1.4 =
* Optimized cache preload scheduling: WP Cron now actively advances the queue on each trigger, preventing queue stalls from unreliable single-event renewal

= 1.1.3 =
* Added FastCGI cache preloading: manual preload, auto-preload after cache clear, and background batch progress display

= 1.1.2 =
* Fixed an issue where www/bare-domain config probing could trigger a PHP Warning when open_basedir was enabled
* Updated config file location description

= 1.0.0 =
* Initial release
* FastCGI cache management
* Disable update checks / disable file editing
* Admin bar cache clear button
* Auto-clear cache on publish / update
* Two-way API sync with the panel
