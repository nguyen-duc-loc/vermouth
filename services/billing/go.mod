module github.com/nguyen-duc-loc/vermouth/services/billing

go 1.27

// The shared module carries no independent version: it moves in lockstep with
// the services importing it, which is the reason for one repository (STK-24).
// This is also why a service image builds with the repository root as its
// Docker build context: the import cannot resolve from this directory alone.
replace github.com/nguyen-duc-loc/vermouth/pkg/vermouth => ../../pkg/vermouth

require (
	github.com/nguyen-duc-loc/vermouth/pkg/vermouth v0.0.0-00010101000000-000000000000
	golang.org/x/sync v0.22.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/twmb/franz-go v1.21.6 // indirect
	github.com/twmb/franz-go/pkg/kadm v1.18.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
