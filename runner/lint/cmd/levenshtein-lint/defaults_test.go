package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/lintcmd"
)

func TestShippedChecksMatchToolchain(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "toolchain.json"))
	if err != nil {
		t.Fatal(err)
	}
	var toolchain struct {
		Checks []string `json:"checks"`
	}
	if err := json.Unmarshal(data, &toolchain); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(shippedChecks, toolchain.Checks) {
		t.Errorf("shippedChecks = %q, want runner/toolchain.json's %q", shippedChecks, toolchain.Checks)
	}
}

func TestALintRunWithoutChecksSelectsTheShippedRules(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"./..."}, shippedChecks},
		{[]string{"-f=json", "./..."}, shippedChecks},
		{[]string{"-checks=inherit", "./..."}, nil},
		{[]string{"-checks=all,gocognit", "./..."}, []string{"all", "gocognit"}},
		{[]string{"-list-checks"}, nil},
		{[]string{"-explain", "SA4006"}, nil},
	} {
		command := lintcmd.NewCommand("levenshtein-lint")
		command.ParseFlags(test.args)

		err := selectShipped(command.FlagSet())

		if err != nil {
			t.Fatalf("selectShipped(%v): %v", test.args, err)
		}
		if got := checkList(command.FlagSet()); !slices.Equal(got, test.want) {
			t.Errorf("checks after selectShipped(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}
