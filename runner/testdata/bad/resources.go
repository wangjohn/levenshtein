package bad

import (
	"context"
	"database/sql"
	"net/http"
	"os/exec"
)

// bodyclose: the response body is never closed.
// noctx: the request carries no context.
func Fetch(url string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	_ = response.StatusCode
	return nil
}

// sqlclosecheck: the rows are never closed.
func Query(db *sql.DB) error {
	rows, err := db.Query("SELECT 1")
	if err != nil {
		return err
	}
	return rows.Err()
}

// rowserrcheck: the iteration never checks Rows.Err.
func Scan(db *sql.DB) (int, error) {
	rows, err := db.Query("SELECT 1")
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		count++
	}
	return count, nil
}

// list runs a command under a context of its own.
func list(dir string) ([]byte, error) {
	return exec.CommandContext(context.Background(), "ls", dir).Output()
}

// contextcheck: cancelling the caller's context does not stop the command
// list runs.
func List(ctx context.Context, dir string) ([]byte, error) {
	return list(dir)
}
