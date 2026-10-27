# Import plugin ESLint 10 source compatibility review

ESLint 10 removes `context.parserOptions`. The current public import plugin
2.32.0 performs `'sourceType' in context.parserOptions` without checking whether
that object exists. Its source can throw before reaching the modern
`context.languageOptions.sourceType` fallback.

The proposed production patch guards the existing legacy branch:

```diff
-  if ('sourceType' in context.parserOptions) {
+  if (context.parserOptions && 'sourceType' in context.parserOptions) {
```

The owned hosted review binds every original publisher source blob at commit
`01c9eb04331d2efa8d63f2d7f4bfec3bc44c94f3` / tree
`1db645f82530ef11adff0d8fb17824144b7f7130`. Only this production function changes.
All original cases, helpers, fixtures, assertions, commands and publisher package
metadata remain unchanged. No peer range is widened. The publisher MIT license
is retained in the review artifact.

Source receipts record native Windows symlink targets separately from their
canonical POSIX Git representation. Only native link separators are translated
for the Git-blob hash comparison; the exact expected blob hash is still required,
and native targets must remain identical before and after execution. Regular
source/case bytes are compared directly. No publisher file or link is rewritten
to accommodate the source reader.

Six additional cases run unchanged against both the original and proposed
function: three use real ESLint 10.13.0 rule contexts for module/script/CommonJS;
three check that legacy parser-options precedence remains intact. The isolated
component harness resolves and installs normally with strict engine and peer
settings, then retains its complete installed graph and unfiltered audit.

This is a production-function regression review, not full import-plugin,
TypeScript-parser, Azure SDK or application compatibility acceptance. The full
original publisher SDK suites, their other removed APIs and current compiler /
dependency graph remain required. No application dependency, private package,
peer waiver, SDK case edit, main merge, deployment or live-data change is included.

Primary sources:

- [ESLint 10 migration: removed context members](https://eslint.org/docs/latest/use/migrate-to-10.0.0#removal-of-deprecated-context-members)
- [Exact public import-plugin source](https://github.com/import-js/eslint-plugin-import/blob/01c9eb04331d2efa8d63f2d7f4bfec3bc44c94f3/src/core/sourceType.js)
- [Publisher SDK and original test commands](https://github.com/import-js/eslint-plugin-import/blob/01c9eb04331d2efa8d63f2d7f4bfec3bc44c94f3/package.json)
