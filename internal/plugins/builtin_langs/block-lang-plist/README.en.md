# Property List

Official Block language extension providing syntax highlighting for macOS `.plist` Property List files when opening project folders or writing fenced code blocks.

## Supported Formats

- **XML Property List**: Distinguishes `<key>` names, `<string>` values, `<integer>` / `<real>` numbers, `<true/>` / `<false/>` booleans, `<date>` / `<data>` payloads, XML declarations, and comments.
- **OpenStep ASCII Property List**: Supports `key = "value";` entries, hexadecimal `<data>` literals, numbers, and `/* ... */` / `//` comments.

## File Matching & Code Blocks

- Files: `*.plist`, `Info.plist`
- Markdown code block tags: `plist`, `property-list`, `apple-plist`

## Notes

This is a purely declarative language extension. It runs directly inside Block with zero background processes and requires no host permissions.
