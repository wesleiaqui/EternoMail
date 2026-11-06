# Bolt Performance Journal

## 2026-11-06 - O(1) Folder-ID Indexing, Viewport Prefetching, and Thread Snapshot Cache
**Learning:** Traversing nested folder trees via recursive search on every conversation row caused unnecessary repetitive tree walks during folder listing. Furthermore, switching between conversations caused a blank loading flash and layout shift because `ConversationViewer` destroyed its message state on every thread change.
**Action:** Indexed all account folders by ID in `accountStore.folderIndex` for instant O(1) name lookup in rows. Introduced a bounded in-memory snapshot cache (capped at 6 threads) in `ConversationViewer` to immediately paint previously opened conversations with zero latency, alongside debounced hover and top-of-viewport body prefetching.

## 2026-11-06 - In-Place Token Scanning for FTS5 Query Preparation
**Learning:** `prepareFTSQuery` processes search terms for FTS5 queries in conversation and virtual folder searches. Splitting queries with `strings.Fields` allocates a slice of sub-strings (`[]string`), and escaping quotes with `strings.ReplaceAll` creates intermediate strings per token. Scanning token boundaries in-place and streaming escaped characters directly into `strings.Builder` reduces allocations from 12 to 5 allocs/op (~58% reduction), cuts allocated memory by 47%, and accelerates token preparation by ~40% (~1339 ns down to ~807 ns).
**Action:** In search and text preparation hot paths, avoid `strings.Fields` and `strings.ReplaceAll` when formatting tokens into a `strings.Builder`; scan boundaries directly and write escaped bytes in-place.

## 2026-11-05 - Multi-Format Timestamp Parsing Dispatch in Database Result Scans
**Learning:** SQLite query scanning across messages and conversations repeatedly runs `parseTimeString` to deserialize timestamps (`date`, `received_at`, `latest_date`). Trying multiple `time.Parse` formats sequentially in a slice loop allocates error objects on each mismatched format (~32 allocations and ~5-9 µs per item batch). By inspecting string length and separator characters (' ', 'T'), the parser can immediately dispatch to the exact layout format, reducing allocations to 0 and execution time by ~8-12x.
**Action:** When parsing database columns with multiple historical date formats, inspect length and signature characters before attempting layout parsing, keeping the sequential loop strictly as a fallback.

## 2026-11-02 - Regex Compilation Caching for Search Match Highlighting
**Learning:** During conversation search (`SearchConversations`, `SearchConversationsByFolderIDs`, and virtual folder queries), `highlightMatches` is executed on multiple display fields (`Subject`, `Snippet`, `FromName`) for every conversation row. Calling `regexp.Compile` on every single field execution resulted in substantial CPU cycles and allocations (over 100+ regex compilations per 50-item search page).
**Action:** In search and result formatting paths where regular expressions are derived from query terms, cache compiled `*regexp.Regexp` objects in a thread-safe, bounded map (capped at 128 queries) so identical queries reuse compiled patterns across all fields and rows.
