# ADR 0033: Short task menus and local file selection

Status: accepted implementation decision, 8 September 2026.

The owner requested fewer upfront options, less typing, and short useful action
explanations. Section menus now show curated common tasks, with specialist tasks
behind a visible Advanced tools entry. All tools continues to expose the complete
implemented registry. Power choices in the short menu reflect observed VM state;
the service remains responsible for authoritative validation. Import begins with
three source types, followed by a file/folder explorer.

The explorer reads local directory names and metadata as the TUI user. It neither
opens image contents nor extracts, imports or creates anything. Choosing a path
only edits its form field. Async directory observations are bounded and carry
unique request tokens; cancellation ignores late results. Symlinks and special
files are not selectable. Existing service checks still govern any later source
observation and mutation. This is navigation, not a new trusted filesystem API.

Forms show one line per field and only the focused field's explanation. Ordinary
forms identify their VM by name; full stable identities remain in details, plans
and confirmation, and requests remain bound to the selected identity. Specialist
mappings still use settings files; this change is not the complete import wizard
or a reduction of any mandatory v1 workflow. New output folders can be typed after
browsing their parent; the picker only selects existing entries.

Owner correction: a blank browser starts in the user's home, never a test-media
folder. An explicit existing field path retains its location. No new ~/.virmill
storage convention is introduced; product-managed files retain the specification's
XDG path contract. The disposable test's ~/images remains fixture input only.
