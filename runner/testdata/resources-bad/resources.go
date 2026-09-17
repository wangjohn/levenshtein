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
	_ = response.StatusCode
	return nil
}

func SQL(db *sql.DB) error {
	rows, err := db.Query("SELECT 1")
	if err != nil {
		return err
	}
	return rows.Err()
}
