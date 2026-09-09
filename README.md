# scstrip

Extract source from a repo, pruning C# conditional-compilation branches that are
unreachable under a given set of symbols. Only reachable code is shipped to the
output directory.

## Build

```
go build -o scstrip ./cmd/scstrip
```

## Project layout

```
cmd/scstrip/          CLI entrypoint (flag parsing only)
internal/scstrip/     stripping engine: #if evaluation, asmdef filtering, real-code
                       detection — importable as a library within this module
.github/workflows/    CI (build/vet/test) and tagged release builds
```

## Usage

```
scstrip -in <src-dir> -out <out-dir> -define USE_IAP -maybe UNITY_IOS -maybe UNITY_STANDALONE_OSX
```

| Flag        | Meaning                                                          |
|-------------|-----------------------------------------------------------------|
| `-in`       | Input repo/source directory (required).                         |
| `-out`      | Output directory for the extracted tree (required).             |
| `-define`   | An **ON** symbol; repeatable.                                   |
| `-defines`  | Comma-separated ON symbols, in addition to `-define`.          |
| `-maybe`    | A **MAYBE** symbol; repeatable.                                 |
| `-maybes`   | Comma-separated MAYBE symbols, in addition to `-maybe`.        |
| `-ext`      | Extensions to ship (default `.cs`); other files are dropped.    |
| `-dry-run`  | Report counts without writing output.                          |

Only files matching `-ext` are written to `-out` (mirroring their relative path);
everything else is dropped. A file left with no real code after stripping is dropped
too — one containing only a BOM, whitespace, comments, `using`/`extern alias`
directives, or empty `namespace` wrappers. A file with any declaration (even an empty
`class`/`interface`/`struct`) is kept. `.git` and the output dir itself are skipped.

The output directory is not cleaned before writing, so delete it first when re-running
to avoid leaving stale files from a previous run.

## Symbol states

Each symbol is in one of three states:

- **ON** (`-define X`): definitely defined. Conditionals resolve to the taken branch
  and the `#if/#elif/#else/#endif` lines are removed — like a normal C# define.
- **MAYBE** (`-maybe X`): may be defined. Every branch that is reachable under some
  combination of MAYBE values is kept, and the directive structure is preserved (dead
  branches pruned) so the output stays valid C#. Use this for mutually exclusive
  platform symbols you want to keep together.
- **OFF** (anything not listed): definitely undefined. Its branches are removed.

If a symbol is passed as both, ON wins.

### Example

```csharp
#if UNITY_STANDALONE_WIN
    return WinPath();
#elif UNITY_STANDALONE_OSX
    return OsxPath();
#elif UNITY_IOS
    return IosPath();
#else
    return DefaultPath();
#endif
```

`-maybe UNITY_STANDALONE_OSX -maybe UNITY_IOS` keeps the OSX, iOS and `#else` branches
(WIN is OFF, so it is pruned) with the `#if/#elif/#else` structure intact. Using
`-define UNITY_IOS` instead resolves the whole chain to just `return IosPath();`.

## Unity asmdef filtering

Before processing a file, the assembly that owns it (its nearest ancestor `.asmdef`)
is checked. If that assembly definitely cannot compile under the symbols, every file
under it is dropped:

- **`defineConstraints`**: dropped when the constraints can't be satisfied — e.g. a
  constraint on an OFF symbol, or `!X` where `X` is ON. Constraints on MAYBE symbols
  keep the assembly.
- **`includePlatforms`**: dropped when none of the listed platforms may be built
  (platform names map to `UNITY_*` symbols; `Editor` → `UNITY_EDITOR`). Editor-only
  assemblies are dropped unless `UNITY_EDITOR` is ON/MAYBE.
- **`excludePlatforms`**: dropped when every platform you're building is excluded.

Files with no ancestor `.asmdef` belong to the predefined assembly and are always kept.

## Notes

Supported `#if` expressions: symbols, `true`/`false`, `!`, `&&`, `||`, `==`, `!=`, and
parentheses (C# precedence). Original line endings are preserved.

In-file `#define`/`#undef` are honored in document order: a `#define X` marks `X` ON
for the code after it, `#undef X` marks it OFF. Those inside a kept MAYBE branch stay
local to that branch and do not leak past its `#endif`. The `#define`/`#undef` lines
themselves are kept in the output, as are other non-conditional directives (`#region`,
`#pragma`, ...).

Directives written inside `/* */` block comments or verbatim strings (`@"..."`) are
treated as text, not directives, so commented-out `#if`/`#endif` blocks are left alone.

Files with genuinely unbalanced directives are copied unchanged with a warning. A single
`#if` chain is limited to 20 distinct MAYBE symbols.
