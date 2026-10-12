# AutoRepairOS

AutoRepairOS is a Go and SQLite application for independent auto repair shops. Each shop should run its own deployment, configuration, and database volume.

## Run locally

Requirements: Go 1.25 or later.

Set the required values in the process environment before starting. For example, in PowerShell:

```powershell
$env:RUN_SEED = "true"
$env:DEFAULT_OWNER_PASSWORD = "<unique-password>"
go run .
```

The application listens on port `8080` by default. After creating the initial owner, set `RUN_SEED=false` and clear `DEFAULT_OWNER_PASSWORD`. `.env` is loaded by Docker Compose; it is not automatically loaded by `go run`.

To build a Windows executable from the current source, run:

```powershell
.\scripts\build-release.ps1
```

The script writes `release\AutoRepairOS-YYYY-MM-DD.exe` (or `AutoRepairOS-<version>.exe` when passed `-Version <version>`), the required `web` assets, an editable `.env` file, and a `start.cmd` launcher. Edit `.env` and double-click `start.cmd` to launch the standalone Windows build. The `release` directory is a local build output and is excluded from Git; it is not an installer. Docker Compose remains the recommended installation because it keeps each shop's database in a dedicated persistent volume.

## Run with Docker Compose

Requirements: Docker Engine with Docker Compose v2. On Windows 11, use Docker Desktop with its WSL2 backend and Linux containers.

1. Copy `.env.example` to `.env`; set a unique `COMPOSE_PROJECT_NAME`, the desired `HOST_PORT`, and an initial owner password.
2. Start the instance:

```powershell
.\deploy.cmd
```

The Linux VPS helper is `scripts/deploy.sh`. Each shop must use a distinct Compose project name and its own `.env`; never share a database volume between shops. The database schema and migrations are in `internal/db/migration/` and are embedded into the Go application.

For the Traditional Chinese Windows installation, SQLite initialization, daily operation, backup, and restore steps, see the [user installation and operation guide](docs/USER_GUIDE_zh-TW.md).

## Configuration

`.env.example` lists supported settings. Keep real credentials in the ignored `.env` file; do not commit secrets, customer data, or database files.

## License

Licensed under the GNU Affero General Public License v3.0. See [`LICENSE`](LICENSE).
