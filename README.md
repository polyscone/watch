# Readme

## Building

Build with `go build` or install with `go install`.

## Usage

Running `watch` will watch all files with the default extensions in the current directory tree. It will run any following commands each time a file changes.

Commands are all space separated arguments after the flags.

There is a special shorthand for make targets. You can specify a command starting with `make:` followed by comma-separated target names.

The `-clear` flag will reset the terminal state with `\033c` before running commands.

The `-exts` flag specifies a space-separated list of file extensions to watch. You can include the default set of extensions by adding a `+ ` prefix to the string, for example: `-exts "+ .mjs .txt"`.

Any patterns given in the `-patterns` or `-skip-patterns` flags are matched using Go's `filepath.Match()` function.

Skip checks are run first and directories that return true for any skip checks are skipped entirely. Watch checks are always done after skip checks.

See `-help` for more.

Examples:
```sh
# Clear the terminal before running commands
# Run: go run foo.go → make build → make run
watch -clear "go run foo.go" "make build" "make run"

# Set extensions to watch
# Run: go run foo.go
watch -exts ".mjs .zig" "go run foo.go"

# Clear the terminal before running commands
# Use SIGTERM instead of SIGKILL on linux/mac
# Run: make build → make test → make run
watch -clear -sigterm "make:build,test,run"
```
