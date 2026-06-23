# go-pacs

Go desktop GUI port of `rusty-dicom-node`, built on the local `../dicom-go` module.

Current slice:

- starts a native Fyne desktop app
- opens a Part 10 DICOM file for local inspection
- imports files or folders into a local SQLite archive with configurable file, ZIP, and traversal safety limits
- stores validated DICOM objects by SHA-256
- adds, edits, deletes, and stores remote node definitions in `nodes.json`
- verifies remote nodes with DICOM C-ECHO through `dicom-go`
- runs Study Root study, series, and image C-FIND queries against configured remote nodes
- retrieves selected query-result studies, series, and images plus selected local series and images with DICOM C-MOVE into the local archive, with progress updates and cancellation
- sends selected local studies, series, and images to a configured node with DICOM C-STORE
- receives inbound C-STORE objects into the local archive with the built-in receiver or standalone receiver command, including configured object-size limits
- stores local AE, receiver address, AE alias, and safety-limit settings in `config.json`
- filters local studies and loaded series, and exports the current study plus loaded series and image lists as CSV or JSON
- parses metadata through `dicom-go` with pixel data skipped
- displays local studies plus patient, study, transfer syntax, and element summaries, including selected archived images
- persists completed import, query, send, retrieve, and receiver summaries with JSON detail in the Tasks tab

Run locally:

```bash
go run ./cmd/pacs-gui
```

Use `-archive-dir /path/to/archive` to choose a catalog/object-store location.

Build a double-clickable macOS app bundle:

```bash
./scripts/build-dist.sh
```

The script uses `go-pacs.png` as the application icon and writes `dist/Go PACS.app`.
You can also run `make package` for the same build.

Cross-platform packaging helpers are also available:

```bash
./scripts/build-linux.sh
./scripts/build-windows.ps1
```

See [SECURITY.md](SECURITY.md) for trusted-network assumptions, receiver allowlists, local PHI handling, export cautions, and import/store limits.

Run the receiver without the GUI:

```bash
go run ./cmd/pacs-receiver
```

The standalone receiver uses the archive `config.json` and `nodes.json` allowlists by default. Use `-address`, `-ae`, or `-no-allowlist` for explicit overrides.

Troubleshooting C-MOVE retrieve:

- C-MOVE only sends a Move Destination AE title to the remote node; the remote node must already know which IP and port belong to that AE.
- For retrieval into this app, configure the remote PACS/viewer destination for `GOPACS` to this Mac's LAN IP and the same port used by the `go-pacs` receiver.
- For remote machines, set the GUI receiver address to an externally reachable address such as `0.0.0.0:11113`, not `127.0.0.1`.

Validate:

```bash
make check
```

License: Apache 2.0. See [LICENSE](LICENSE).
