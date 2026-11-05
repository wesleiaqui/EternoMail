# Bolt Performance Journal

## 2026-11-05 - Multi-Format Timestamp Parsing Dispatch in Database Result Scans
**Learning:** SQLite query scanning across messages and conversations repeatedly runs `parseTimeString` to deserialize timestamps (`date`, `received_at`, `latest_date`). Trying multiple `time.Parse` formats sequentially in a slice loop allocates error objects on each mismatched format (~32 allocations and ~5-9 µs per item batch). By inspecting string length and separator characters (' ', 'T'), the parser can immediately dispatch to the exact layout format, reducing allocations to 0 and execution time by ~8-12x.
**Action:** When parsing database columns with multiple historical date formats, inspect length and signature characters before attempting layout parsing, keeping the sequential loop strictly as a fallback.

## 2026-11-02 - Regex Compilation Caching for Search Match Highlighting
**Learning:** During conversation search (`SearchConversations`, `SearchConversationsByFolderIDs`, and virtual folder queries), `highlightMatches` is executed on multiple display fields (`Subject`, `Snippet`, `FromName`) for every conversation row. Calling `regexp.Compile` on every single field execution resulted in substantial CPU cycles and allocations (over 100+ regex compilations per 50-item search page).
**Action:** In search and result formatting paths where regular expressions are derived from query terms, cache compiled `*regexp.Regexp` objects in a thread-safe, bounded map (capped at 128 queries) so identical queries reuse compiled patterns across all fields and rows.
