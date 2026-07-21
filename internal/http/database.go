package http

import (
	"errors"
	"fmt"

	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"
)

func requestDB(ctx *cartridge.Context) (*gorm.DB, error) {
	if ctx.DBManager == nil {
		return nil, errors.New("request database manager is unavailable")
	}
	db, err := ctx.DBManager.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect request database: %w", err)
	}
	if db == nil {
		return nil, errors.New("connect request database: connection is unavailable")
	}
	return db.WithContext(ctx.UserContext()), nil
}
