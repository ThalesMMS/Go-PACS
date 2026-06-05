# Go-PACS

A Picture Archiving and Communication System (PACS) implementation in Go with a modern GUI built with Fyne.

## Features

- DICOM image storage and retrieval using [dicom-go](https://github.com/ThalesMMS/dicom-go)
- Modern cross-platform GUI
- Network receiver for DICOM data
- SQLite database for metadata storage

## Requirements

- Go 1.22 or higher
- Fyne tools (for packaging)

## Installation

### Clone the repository

```bash
git clone https://github.com/ThalesMMS/Go-PACS.git
cd Go-PACS
```

### Install dependencies

```bash
go mod download
```

## Building

### Build for macOS

```bash
./scripts/build-dist.sh
```

### Build for Linux

```bash
./scripts/build-linux.sh
```

### Build for Windows

```powershell
.\scripts\build-windows.ps1
```

### Manual build

To build without packaging:

```bash
go build -o pacs-gui ./cmd/pacs-gui
go build -o pacs-receiver ./cmd/pacs-receiver
```

## Usage

### GUI Application

Run the GUI application:

```bash
./pacs-gui
```

### Receiver Service

Run the DICOM receiver service:

```bash
./pacs-receiver
```

## Development

### Run tests

```bash
go test ./...
```

### Format code

```bash
make fmt
```

### Run linter

```bash
make vet
```

### Full check

```bash
make check
```

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Author

Thales Matheus Mendonça Santos

## Acknowledgments

- Built with [Fyne](https://fyne.io/) - a cross-platform GUI toolkit for Go
- Uses [dicom-go](https://github.com/ThalesMMS/dicom-go) for DICOM parsing
