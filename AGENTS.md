# Agent preferences

## Code readability

- Use one blank line between top-level declarations and between distinct logical sections of a function, including test setup, execution, and assertions.
- Keep closely related statements together, especially an operation and its error check. Do not separate every statement or add multiple blank lines just for spacing.
- Keep spacing modest and consistent with the language's formatter (`gofmt` for Go). Do not edit generated code for style.

## Go value modeling

- Use defined string types and typed constants for finite choices such as statuses and check kinds. Keep free-form text as strings.
- Mark completed value records with `//levenshtein:record`. Compute their fields first and construct them together in a struct literal; reserve field mutation for state-bearing objects.
- Run the shared [Go lint rules](docs/go-lint.md) when changing Go code.
