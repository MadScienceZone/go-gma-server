module github.com/MadScienceZone/go-gma-server

go 1.18

replace github.com/MadScienceZone/go-gma/v4 => ../go-gma

require (
	github.com/google/go-cmp v0.5.5
	github.com/mattn/go-sqlite3 v1.14.6
	github.com/schwarmco/go-cartesian-product v0.0.0-20180515110546-d5ee747a6dc9
)

require (
	github.com/MadScienceZone/go-gma/v4 v4.0.0
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
)
