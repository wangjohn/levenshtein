package resources

import (
	"context"
	"database/sql"
	"net/http"
)

func HTTP(ctx context.Context, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return nil
}

func SQL(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT 1")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	return rows.Err()
}
