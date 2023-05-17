module trc-basic

go 1.20

require github.com/peterbourgon/trc v0.0.0-00010101000000-000000000000

require (
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/oklog/ulid/v2 v2.1.0 // indirect
	github.com/peterbourgon/unixtransport v0.0.1 // indirect
	golang.org/x/exp v0.0.0-20230425010034-47ecfdc1ba53 // indirect
)

replace github.com/peterbourgon/trc => ../..
