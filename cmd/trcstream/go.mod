module github.com/peterbourgon/trc/cmd/trcstream

go 1.20

replace github.com/peterbourgon/trc => ../../

replace github.com/peterbourgon/ff/v4 => ../../../ff

require (
	github.com/oklog/run v1.1.0
	github.com/peterbourgon/ff/v4 v4.0.0-alpha.1
	github.com/peterbourgon/trc v0.0.0-00010101000000-000000000000
)

require (
	github.com/bernerdschaefer/eventsource v0.0.0-20130606115634-220e99a79763 // indirect
	github.com/oklog/ulid/v2 v2.1.0 // indirect
)
