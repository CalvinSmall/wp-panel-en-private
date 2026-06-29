=== WP Panel Optimizer ===
Contributors: naibabiji
Requires at least: 5.0
Tested up to: 7.0
Requires PHP: 8.1
Stable tag: 1.1.5
License: GPL-2.0+
License URI: https://www.gnu.org/licenses/gpl-2.0.html

Works with WP Panel to manage FastCGI cache, disable update checks, disable file editing and other optimizations from the WordPress admin, with bidirectional sync with the panel.

== Description ==

WP Panel Optimizer is a companion plugin for [WP Panel](https://github.com/naibabiji/wp-panel) that syncs optimization settings in real-time with the server-side panel through the panel API.

Author: [naibabiji](https://blog.naibabiji.com) | Plugin URL: [GitHub](https://github.com/naibabiji/wp-panel)

= Features =

* **FastCGI Cache Management**: Enable/disable Nginx FastCGI full-site cache from WordPress admin, set cache TTL
* **Cache Preloading**: Manually or automatically visit public pages after cache clear so Nginx generates FastCGI cache files
* **Disable Update Checks**: Completely block WordPress update checks (no red dots, no notifications on dashboard, update check buttons won't work). Turn off this switch to check for updates
* **Disable File Editing**: Write DISALLOW_FILE_EDIT constant to wp-config.php
* **Admin Bar Quick Clear**: One-click Nginx cache clear from WordPress admin bar
* **Auto Cache Clear**: Automatically clear cache when publishing/updating/deleting posts
* **Bidirectional Panel Sync**: Push settings changes to the panel automatically, also pull the latest panel state

= Requirements =

* WP Panel v1.0.0-beta2+ installed
* Plugin is installed automatically by the panel (Site Details → WordPress Optimizations → Install Companion Plugin), no manual upload needed

== Installation ==

1. Go to the site details page in WP Panel
2. In the "WordPress Optimizations" card, check the optimizations you want to enable
3. Click the "Install Companion Plugin" button, the panel will deploy the plugin to wp-content/plugins/
4. Activate the plugin in WordPress admin, or let the panel activate it automatically

After installation, the panel writes the config file (with panel URL and API Key) to /var/wp-panel/site-secrets/<domain>/wp-panel-config.json outside the web root — no manual credential entry needed.

== Changelog ==

= 1.1.5 =
* Added "Clear Nginx Cache" button on plugin settings page for convenient cache clearing from mobile admin

= 1.1.4 =
* Optimized cache preload scheduling: system Cron pushes the queue forward whenever WordPress is triggered, preventing queue stalling from unstable single-event renewal

= 1.1.3 =
* Added FastCGI cache preloading: supports manual preload, auto preload after cache clear, and background batch processing status display

= 1.1.2 =
* Fixed potential PHP Warning when open_basedir is enabled and probing www/bare domain config
* Updated config file location description

= 1.0.0 =
* Initial release
* FastCGI cache management
* Disable update checks / disable file editing
* Admin bar cache clear button
* Auto cache clear on publish/update posts
* Bidirectional sync with panel API
