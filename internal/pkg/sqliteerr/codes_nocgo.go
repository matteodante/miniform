//go:build !cgo

package sqliteerr

func hasContentionCode(error) bool      { return false }
func hasUniqueCode(error) bool          { return false }
func hasUniqueOrPrimaryCode(error) bool { return false }
