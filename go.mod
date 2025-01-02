module shop-noise-sonos-control

go 1.23

toolchain go1.23.4

require (
	github.com/RobinUS2/golang-moving-average v1.0.0
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/cdzombak/asyncerror v0.0.0-20241220181401-53d6fbd3ba6d
	github.com/ianr0bkny/go-sonos v0.0.0-20171025003233-056585059953
	github.com/influxdata/influxdb-client-go/v2 v2.14.0
	github.com/pkg/errors v0.9.1
	gobot.io/x/gobot v1.16.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/gofrs/uuid v4.0.0+incompatible // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.0.0 // indirect
	github.com/influxdata/line-protocol v0.0.0-20200327222509-2487e7298839 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/oapi-codegen/runtime v1.0.0 // indirect
	github.com/sigurn/crc8 v0.0.0-20160107002456-e55481d6f45c // indirect
	github.com/sigurn/utils v0.0.0-20190728110027-e1fefb11a144 // indirect
	golang.org/x/net v0.23.0 // indirect
	periph.io/x/periph v3.6.2+incompatible // indirect
)

replace github.com/RobinUS2/golang-moving-average => github.com/cdzombak/golang-moving-average v0.0.0-20230705193950-473b9570a2cb
