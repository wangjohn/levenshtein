package resources

import (
	"database/sql"
	"net/http"
)

func HTTP(url string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return nil
}

func SQL(db *sql.DB) error {
	rows, err := db.Query("SELECT 1")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	return rows.Err()
}
