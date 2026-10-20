# profile/

Profiles live in the database: each user writes theirs in the dashboard's
setup flow (or has the AI draft it from their resume) and edits it on the
Profile page. This folder is only for moving a profile in or out of the
database by hand as a markdown file.

| File | What it is |
| --- | --- |
| `profile.example.md` | The template, with the sections Wera expects. Tracked. |
| `*.md` (anything else) | Your own profiles. Gitignored: they hold your resume. |

Import one (replaces that user's profile text; their preferences and
setup answers stay as they are, and their jobs are rescored on the next
run because the text's hash changed):

```bash
cp profile/profile.example.md profile/you.md     # then fill it in
go run ./cmd/wera users import-profile --email you@example.com --file profile/you.md
```

Export one (a backup before editing, or to move it to another server):

```bash
go run ./cmd/wera users export-profile --email you@example.com --file profile/you.md
go run ./cmd/wera users export-profile --email you@example.com   # to stdout
```

In Docker the folder is mounted read-only at `/app/profile`, so imports
work from the containers (`docker exec wera-api /app/wera users import-profile ...`);
export to stdout there and redirect on the host.
