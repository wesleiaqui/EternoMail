# Bolt Performance Journal

## 2026-11-02 - Regex Compilation Caching for Search Match Highlighting
**Learning:** During conversation search (`SearchConversations`, `SearchConversationsByFolderIDs`, and virtual folder queries), `highlightMatches` is executed on multiple display fields (`Subject`, `Snippet`, `FromName`) for every conversation row. Calling `regexp.Compile` on every single field execution resulted in substantial CPU cycles and allocations (over 100+ regex compilations per 50-item search page).
**Action:** In search and result formatting paths where regular expressions are derived from query terms, cache compiled `*regexp.Regexp` objects in a thread-safe, bounded map (capped at 128 queries) so identical queries reuse compiled patterns across all fields and rows.
