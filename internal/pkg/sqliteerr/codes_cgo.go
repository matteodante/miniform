//go:build cgo

package sqliteerr

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

func hasContentionCode(err error) bool {
	var driverError sqlite3.Error
	return errors.As(err, &driverError) && (driverError.Code == sqlite3.ErrBusy || driverError.Code == sqlite3.ErrLocked)
}

func hasUniqueCode(err error) bool {
	var driverError sqlite3.Error
	return errors.As(err, &driverError) && driverError.ExtendedCode == sqlite3.ErrConstraintUnique
}

func hasUniqueOrPrimaryCode(err error) bool {
	var driverError sqlite3.Error
	return errors.As(err, &driverError) &&
		(driverError.ExtendedCode == sqlite3.ErrConstraintUnique || driverError.ExtendedCode == sqlite3.ErrConstraintPrimaryKey)
}

func hasForeignKeyCode(err error) bool {
	var driverError sqlite3.Error
	return errors.As(err, &driverError) && driverError.ExtendedCode == sqlite3.ErrConstraintForeignKey
}
