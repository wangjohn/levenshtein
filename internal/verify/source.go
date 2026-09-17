package verify

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
)

func daggerIncludes(inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("Dagger checks require explicit input paths")
	}

	var includes []string
	for _, input := range inputs {
		if !relative(input) || strings.ContainsAny(input, "*?[]{}!\n\r") {
			return nil, fmt.Errorf("Dagger input %q must be a literal repository-relative path without pattern characters", input)
		}
		if input == "." {
			includes = append(includes, "**")
		} else {
			path := filepath.ToSlash(input)
			includes = append(includes, path, path+"/**")
		}
	}
	return includes, nil
}

func privateSourcePath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		//lint:ignore LV1001 Filesystem components are arbitrary paths, not an enum.
		if part == ".git" || part == ".env" || (strings.HasPrefix(part, ".env.") && part != ".env.example") {
			return true
		}
	}
	return false
}

// Reject aliases before asking Dagger to import files. A path inside an allowed
// directory must not expose another part of the checkout through a symlink.
func validateDaggerSource(source string, inputs []string) error {
	dir, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	for _, input := range inputs {
		if privateSourcePath(input) {
			continue
		}
		prefix := ""
		for _, part := range strings.Split(input, string(filepath.Separator)) {
			prefix = filepath.Join(prefix, part)
			info, err := dir.Lstat(prefix)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("Dagger input %q contains symlink %q; declare the real input path", input, prefix)
			}
		}

		err := fs.WalkDir(snapshotFS{FS: dir.FS(), root: dir}, filepath.ToSlash(input), func(path string, entry fs.DirEntry, err error) error {
			if privateSourcePath(path) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("Dagger input %q contains symlink %q; declare the real input path", input, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func daggerSource(client *dagger.Client, source string, inputs []string) (*dagger.Directory, error) {
	includes, err := daggerIncludes(inputs)
	if err != nil {
		return nil, err
	}
	if err := validateDaggerSource(source, inputs); err != nil {
		return nil, err
	}
	return client.Host().Directory(source, dagger.HostDirectoryOpts{
		Include: includes,
		Exclude: []string{"**/.env", "**/.env.*", "!**/.env.example", "**/.git"},
	}), nil
}
