module example.com/mod-untidy

go 1.27.1

// Nothing imports this module, so tidy drops the requirement.
require example.com/mod-untidy/unused v0.0.0

replace example.com/mod-untidy/unused => ./unused
