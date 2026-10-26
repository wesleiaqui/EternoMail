package database

import "fmt"

// UnixEpochSQL returns an expression that reads both SQLite/RFC3339 timestamps
// and the legacy Go time.Time strings stored in the database. column must be a
// trusted SQL column expression, never user input.
func UnixEpochSQL(column string) string {
	// Go stores its numeric offset as +0300 after the time (including optional
	// fractional seconds); SQLite expects +03:00 and does not accept zone names.
	offset := fmt.Sprintf("substr(%s, instr(substr(%s, 12), ' ') + 12, 5)", column, column)
	return fmt.Sprintf(`COALESCE(unixepoch(%[1]s), CASE
		WHEN %[2]s GLOB '[+-][0-9][0-9][0-9][0-9]'
		THEN unixepoch(substr(%[1]s, 1, 19) || ' ' || substr(%[2]s, 1, 3) || ':' || substr(%[2]s, 4, 2))
	END)`, column, offset)
}
