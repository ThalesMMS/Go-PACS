# Security Notes

`go-pacs` is intended for trusted local or private DICOM networks unless explicit security controls are added around it.

## Network Trust

- DICOM C-ECHO, C-FIND, C-MOVE, and C-STORE traffic is not encrypted by this app.
- Configure remote nodes only for systems on networks you trust.
- Do not expose the built-in receiver or `cmd/pacs-receiver` directly to the public internet.
- The standalone receiver uses `nodes.json` allowlists by default. Keep `-no-allowlist` for controlled test networks only.

## Receiver Controls

- Receiver settings support Called AE aliases, Calling AE allowlists, remote IP allowlists, and maximum stored object size.
- Unknown callers or disallowed remote hosts are rejected before objects are stored.
- Store-size limits are enforced before writing inbound C-STORE datasets to the archive.

## Local Data

- Imported DICOM objects are copied into the configured archive object store and indexed in SQLite.
- Patient names, patient IDs, accessions, study metadata, local source paths, and UIDs can appear in the local archive database and user-initiated CSV/JSON exports.
- Do not commit archives, exported metadata, screenshots containing patient data, or real patient DICOM files.
- Use synthetic or anonymized fixtures for tests and demos.

## Logging And Summaries

- Operation summaries record counts, statuses, durations, and failure text.
- Avoid adding logs that dump full datasets, pixel data, credentials, or patient-bearing filesystem paths.
- Treat task-history JSON as local operational metadata that may contain node names, UIDs, and failure details.

## Import Limits

- File, folder, ZIP, traversal, ZIP-entry, ZIP-total, and inbound-store size limits are configurable.
- Keep conservative limits for shared workstations and test environments where input files are not fully controlled.
