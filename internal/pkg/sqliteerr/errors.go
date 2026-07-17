package sqliteerr

import "strings"

func IsContention(err error) bool {
	if err == nil {
		return false
	}
	if hasContentionCode(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"database is locked",
		"database is busy",
		"database table is locked",
		"sql statements in progress",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func IsUniqueConstraint(err error) bool {
	return err != nil && hasUniqueCode(err)
}

func IsUniqueOrPrimaryConstraint(err error) bool {
	return err != nil && hasUniqueOrPrimaryCode(err)
}

func IsForeignKeyConstraint(err error) bool {
	if err == nil {
		return false
	}
	return hasForeignKeyCode(err) || strings.Contains(strings.ToLower(err.Error()), "foreign key constraint failed")
}
