# Agent preferences

## Code readability

- Use one blank line between top-level declarations and between distinct logical sections of a function, including test setup, execution, and assertions.
- Keep closely related statements together, especially an operation and its error check. Do not separate every statement or add multiple blank lines just for spacing.
- Declare each struct field on its own line, including fields that share a type.
- Keep spacing modest and consistent with the language's formatter (`gofmt` for Go). Do not edit generated code for style.

## Go value modeling

- Use defined string types and typed constants for finite choices such as statuses and check kinds. Keep free-form text as strings.
- Construct new struct values together in a literal, without opt-in markers. Compute fields first; reserve subsequent field mutation for existing state-bearing objects.
- Run the shared [Go lint rules](docs/go-lint.md) when changing Go code.
