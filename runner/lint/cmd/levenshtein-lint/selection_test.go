package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/lintcmd"
)

// The general cases live in runner/testdata/selection.json, which the
// runner's and the CLI's copies of allowed also load.
func TestSelectionMatchesTheSharedTable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "selection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Cases []struct {
			Name   string   `json:"name"`
			Checks []string `json:"checks"`
			Code   string   `json:"code"`
			Want   bool     `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &shared); err != nil {
		t.Fatal(err)
	}

	for _, test := range shared.Cases {
		if got := allowed(test.Checks, test.Code); got != test.Want {
			t.Errorf("%s: allowed(%v, %q) = %v, want %v", test.Name, test.Checks, test.Code, got, test.Want)
		}
	}
}

func TestCheckListReadsTheChecksFlag(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{nil, nil},
		{[]string{"-checks=inherit"}, nil},
		{[]string{"-checks=all,-ST1000, gocognit"}, []string{"all", "-ST1000", "gocognit"}},
	} {
		command := lintcmd.NewCommand("levenshtein-lint")
		command.ParseFlags(test.args)

		if got := checkList(command.FlagSet()); !slices.Equal(got, test.want) {
			t.Errorf("checkList(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}
