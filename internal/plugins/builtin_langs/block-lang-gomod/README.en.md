# Go Modules

Official Block language extension providing syntax highlighting when opening Go project folders or writing fenced code blocks.

## Supported Files

- `go.mod` / `*.mod`: Highlights `module`, `go`, `toolchain`, `require`, `replace`, `exclude`, `retract`, `use`, and `tool` directives, module paths, semantic versions, `=>` operators, and `// indirect` markers.
- `go.work` / `*.work`: Shares workspace directive and path highlighting with `go.mod`.
- `go.sum` / `go.work.sum` / `*.sum`: Distinguishes module paths, version tags (including `/go.mod`), and `h1:` checksum hashes.

## Code Block Tags

Use any of the following language tags in Markdown notes:

- `gomod`, `go.mod`, `go.work`, `gowork`, `mod`
- `gosum`, `go.sum`, `sum`

## Notes

This is a purely declarative language extension. It runs directly inside Block with zero background processes and requires no host permissions.
