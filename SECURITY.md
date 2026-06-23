# Security Notes

`go-pacs` is intended for trusted local or private DICOM networks unless explicit security controls are added around it.

## Network Trust

- DICOM C-ECHO, C-FIND, C-MOVE, and C-STORE traffic is not encrypted by this app.
- DICOMweb traffic uses HTTP or HTTPS according to how you deploy `cmd/pacs-web`; use HTTPS at the reverse proxy or network boundary when traffic leaves a trusted host.
- Configure remote nodes only for systems on networks you trust.
- Do not expose the built-in receiver or `cmd/pacs-receiver` directly to the public internet.
- Do not expose `/dicomweb` directly to the public internet without a trusted network boundary, HTTPS termination, request-size controls, and operational monitoring.
- The standalone receiver uses `nodes.json` allowlists by default. Keep `-no-allowlist` for controlled test networks only.

## Receiver Controls

- Receiver settings support Called AE aliases, Calling AE allowlists, remote IP allowlists, and maximum stored object size.
- Unknown callers or disallowed remote hosts are rejected before objects are stored.
- Store-size limits are enforced before writing inbound C-STORE datasets to the archive.

## Local Data

- Imported DICOM objects are copied into the configured archive object store and indexed in SQLite.
- DICOMweb QIDO-RS and WADO-RS responses can expose patient names, patient IDs, accessions, study metadata, UIDs, and full Part 10 objects to authorized bearer tokens.
- DICOMweb STOW-RS imports accepted Part 10 objects into the same local archive and SQLite catalog used by GUI and DIMSE workflows.
- Patient names, patient IDs, accessions, study metadata, local source paths, and UIDs can appear in the local archive database and user-initiated CSV/JSON exports.
- Do not commit archives, exported metadata, screenshots containing patient data, or real patient DICOM files.
- Use synthetic or anonymized non-PHI fixtures for tests, demos, smoke-test scripts, issue comments, logs, and documentation examples.

## DICOMweb Service Tokens

- `/dicomweb/capabilities` is unauthenticated and returns a non-PHI capabilities document.
- QIDO-RS and WADO-RS routes require bearer service tokens with `read` or `write` role.
- STOW-RS routes require bearer service tokens with `write` role.
- Service tokens are generated locally, the plaintext value is shown only at creation time, and only token hashes are persisted in `tokens.json`.
- Treat plaintext service tokens like credentials. Do not paste them into issues, logs, screenshots, fixture files, or docs.
- Token revocation is local to the archive token store. Rotate tokens when a workstation, script, or integration is no longer trusted.
- OAuth/OIDC, SAML, enterprise IAM integration, delegated user login, external authorization policy engines, and mTLS client identity are not implemented auth models.

## Logging And Summaries

- Operation summaries record counts, statuses, durations, and failure text.
- DICOMweb audit records include request ID, token ID, remote address, method/path operation, status, byte count, and a bounded error summary.
- Audit records intentionally do not store plaintext bearer tokens, token hashes, full DICOM datasets, or Pixel Data.
- Avoid adding logs that dump full datasets, pixel data, credentials, or patient-bearing filesystem paths.
- Treat task-history JSON as local operational metadata that may contain node names, UIDs, and failure details.

## Import Limits

- File, folder, ZIP, traversal, ZIP-entry, ZIP-total, and inbound-store size limits are configurable.
- Keep conservative limits for shared workstations and test environments where input files are not fully controlled.
