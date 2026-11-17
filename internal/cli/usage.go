package cli

import (
	"fmt"
	"os"
)

// entryUsage builds the usage of a command that has no longer text of its
// own: its entry from the top-level help, then details, which names the flags
// and arguments the handler parses.
func entryUsage(name, details string) func() {
	return func() {
		fmt.Print("Usage:\n" + commandHelpEntry(name))
		if details != "" {
			fmt.Print("\n" + details)
		}
	}
}

// serverAddressUsage ends the usage of every command that asks a running
// server; getServerURL is what reads these variables.
const serverAddressUsage = `
The server is found through DEPSILO_URL, then DEPSILO_PORT on localhost
(default http://localhost:23333).
`

const jsonFlagUsage = `    --json, -j           Machine-readable JSON output
`

func printServeUsage() {
	// The flag set owns the text; asking it for help prints it.
	_, _ = ParseServeFlags([]string{"--help"}, os.Stdout)
}

func printStartUsage() {
	entryUsage("start", `Without --daemon the server runs in the foreground, exactly like
"depsilo serve". With --daemon it starts in the background, waits until the
server is ready and returns; "depsilo stop" stops it.

Flags:
    --daemon, -d         Run in the background; the startup log is
                         ~/.local/share/depsilo/depsilo.log

Every other flag is a "depsilo serve" flag:

`)()
	printServeUsage()
}

const stopUsageDetails = `Stops the daemon that "depsilo start --daemon" started and waits for it to
exit. Takes no flags or arguments.
`

const statusUsageDetails = `Flags:
` + jsonFlagUsage + serverAddressUsage

const doctorUsageDetails = `Flags:
` + jsonFlagUsage + serverAddressUsage

const diagnoseUsageDetails = `Flags:
    --out, -o FILE       Where to write the report (default:
                         depsilo-diagnostic.json in the working directory);
                         an existing file is never overwritten
` + jsonFlagUsage + serverAddressUsage

const versionUsageDetails = `Flags:
` + jsonFlagUsage

const activateUsageDetails = `Flags:
    --shell, -s SHELL    Shell syntax to print: fish, or any other value for
                         POSIX "export" lines (default bash)
    --eco, -e LIST       Comma-separated ecosystems to configure, from: pypi,
                         npm, go, cargo, apt, maven, conda (default: all)

Nothing is sent to the server; the address written into the variables comes
from DEPSILO_URL, then DEPSILO_PORT on localhost (default
http://localhost:23333).

Examples:
    eval "$(depsilo activate)"
    source (depsilo activate --shell fish | psub)
    depsilo activate --eco pypi,npm
`

const warmupUsageDetails = `Arguments:
    <eco>                Ecosystem of the packages, for example pypi or npm
    <pkg> [pkg...]       One or more package names

Flags:
` + jsonFlagUsage + `
Examples:
    depsilo warmup pypi requests
    depsilo warmup pypi requests numpy torch
    depsilo warmup npm express react
` + serverAddressUsage + `The request needs an administrator token: DEPSILO_TOKEN, or the file
~/.config/depsilo/token.
`

const flushUsageDetails = `Flags:
` + jsonFlagUsage + serverAddressUsage + `The request needs an administrator token: DEPSILO_TOKEN, or the file
~/.config/depsilo/token.
`

const backupUsageDetails = `Flags:
    --out, -o FILE       Archive to write (default: depsilo-backup.tar.gz in
                         the working directory)
` + jsonFlagUsage + `
The archive holds config.toml, the instance's signing key and a consistent
SQLite snapshot; cache objects are not included. The server may keep
running. The configuration is found like the server finds it (DEPSILO_CONFIG,
then the standard search paths).
`

const restoreUsageDetails = `Arguments:
    <backup.tar.gz>      Archive written by "depsilo backup"

Flags:
    --config-target PATH     Where to restore config.toml (default: the
                             current configuration's path)
    --database-target PATH   Where to restore the SQLite database (default:
                             the current configuration's database)
    --json, -j               Machine-readable JSON output

The Depsilo server that owns the target database must be stopped. To recover
from a broken config.toml, pass both --config-target and --database-target.
`

const auditUsageDetails = `Subcommands:
    verify               The only subcommand; takes no arguments

Flags:
` + jsonFlagUsage + `
Runs offline against the SQLite file of the local configuration (DEPSILO_CONFIG,
then the standard search paths); no server needs to run. Exits 1 when the
chain or its anchors do not verify.
`
