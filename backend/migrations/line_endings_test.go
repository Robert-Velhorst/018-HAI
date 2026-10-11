package migrations

import "strings"

func normalizeMigrationLineEndings(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}
