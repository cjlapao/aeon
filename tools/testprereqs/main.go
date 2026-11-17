// testprereqs checks the driver linked into this build without opening user state.
package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/cjlapao/aeon/internal/database"
)

func main() {
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, ":memory:")
	if err == nil {
		defer db.Close()
		err = database.CheckFeatures(db)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "test prerequisites:", err)
		os.Exit(1)
	}
}
