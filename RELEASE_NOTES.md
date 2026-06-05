# Release Notes

## Unreleased

### Security and Isolation Hardening

- Added per-site WordPress object cache isolation for Redis-backed caches. New and upgraded WordPress sites now get both `WP_REDIS_PREFIX` and `WP_CACHE_KEY_SALT` in `wp-config.php`, preventing Redis object cache keys from colliding across multiple sites on the same server.
- Added an upgrade migration to backfill missing cache prefixes for existing WordPress sites without overwriting user-defined values.
- Changed internal site resource names to include a stable short hash, avoiding collisions between domains such as `ab.com` and `a-b.com` when generating system users, database names, and related resources.
- Added resource availability checks before site creation so existing system users, web roots, log directories, PHP-FPM pools, Nginx configs, sockets, database names, and database users are detected before provisioning starts.
- Randomized the WordPress database table prefix for new and reinstalled WordPress sites, replacing the fixed `wp_` prefix with a generated prefix such as `wp_a1b2c3d4_`.

### Tests

- Added coverage for cache prefix insertion, preserving existing cache prefix values, resource name collision avoidance, normalized domain handling, resource name length limits, and random WordPress table prefix format.
