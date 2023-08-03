module trc-complex

go 1.20

require (
	github.com/felixge/fgprof v0.9.3
	github.com/peterbourgon/trc v0.0.0-00010101000000-000000000000
)

require (
	github.com/bernerdschaefer/eventsource v0.0.0-20130606115634-220e99a79763 // indirect
	github.com/google/pprof v0.0.0-20211214055906-6f57359322fd // indirect
	github.com/oklog/ulid/v2 v2.1.0 // indirect
)

replace github.com/peterbourgon/trc => ../..
