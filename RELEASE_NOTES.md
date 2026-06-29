# Release Notes

## v1.2.24

### New: S3-Compatible Object Storage for Remote Backup

- Added "S3-Compatible Object Storage" backend for remote backups, supporting Cloudflare R2, AWS S3, MinIO, Backblaze B2 and other compatible services.
- Preserved the original SSH / rsync remote backup method; existing rsync-configured servers retain default behavior after upgrade.
- Added backup destination selection in settings, allowing switching between SSH / rsync and S3-compatible object storage.
- S3 configuration supports Endpoint, Bucket, Region, Access Key ID, Secret Access Key, and path prefix.
- Cloudflare R2 can use `auto` as Region with an account-specific R2 Endpoint.

### Improved: Large File Backup Upload

- S3 backend supports multipart upload; large files are automatically split into parts and appear as a single complete backup object in object storage.
- Failed part uploads automatically abort the multipart upload, reducing incomplete part residue on the remote end.
- Small files continue to use single PUT upload to avoid unnecessary multipart overhead.
- S3 file upload timeout adjusted to a longer duration suitable for backup scenarios; connection tests retain short timeout.

### Security & Compatibility

- S3 Endpoint must use HTTPS.
- Added format validation for Bucket, Region, Access Key ID, path prefix, etc.
- Secret Access Key and SSH passwords continue to be masked in API responses.
- Local upload paths remain restricted to the panel backup directory to prevent arbitrary file sync.
- S3 uploads use standard SigV4 signing, no dependency on rclone or additional system commands.

### Database Upgrade

- `remote_backup_settings` now includes S3 remote backup configuration fields.
- Both fresh install and upgrade paths are handled.
- After upgrade, default `backup_type` is `rsync`; backup backend is not automatically switched.

### Testing

- Added remote backup type and S3 parameter validation tests.
- Added database field tests for fresh install and upgrade scenarios.
- Added mock S3 service tests covering connection detection, multipart successful upload, and failed abort.
- Added S3 XML complete request tests verifying `Content-Type: application/xml` and ETag XML format.
