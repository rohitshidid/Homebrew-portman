# portmap

One table for listening ports, owning apps, Docker containers, and known service labels.

```text
PORT   PROTO  ADDRESS    APP       PID   CONTAINER  SERVICE
5432   tcp    127.0.0.1  postgres  1234  pg          postgres
6379   tcp    *          docker     -     redis       redis
27017  tcp    127.0.0.1  mongod     768   -           mongodb
```

`portmap` is intentionally small: it uses the local OS tools that already know about sockets (`lsof` on macOS, `ss`/`netstat` on Linux), then annotates those rows with Docker published ports and `/etc/services` labels.

## Install

From the Homebrew tap:

```sh
brew tap rohitshidid/portman
brew install rohitshidid/portman/portmap
```

The current remote is named `Homebrew-portman`, so Homebrew's tap shorthand is `rohitshidid/portman`. If you rename the GitHub repository to `homebrew-portmap`, the command becomes `brew tap rohitshidid/portmap`.

From a source checkout:

```sh
go install ./cmd/portmap
```

## Usage

```sh
portmap
portmap --port 5432
portmap --json
portmap --protocol tcp
portmap --protocol udp --no-docker
```

Flags:

- `--json`: print JSON instead of a table
- `--no-docker`: skip Docker annotations
- `--port 5432`: show only one listening port
- `--protocol all|tcp|udp`: filter rows by protocol
- `--timeout 5s`: cap the scan duration
- `--version`: print the build version

## Build

```sh
go test ./...
go build -o portmap ./cmd/portmap
```

## Release To Homebrew

1. Commit the source and formula.
2. Tag the release:

   ```sh
   git tag v0.1.0
   git push origin main --tags
   ```

3. Test the formula from the tap:

   ```sh
   brew install --build-from-source rohitshidid/portman/portmap
   brew test rohitshidid/portman/portmap
   ```

New projects should start in a tap. `homebrew/core` has higher requirements for self-submitted software, so this package should live in your own tap until it has enough adoption.
