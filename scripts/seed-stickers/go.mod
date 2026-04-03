module github.com/mst-corp/orbit/services/messaging/scripts/seed-stickers

go 1.24

replace github.com/mst-corp/orbit/pkg => ../../pkg

replace github.com/mst-corp/orbit/services/messaging => ../../services/messaging

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.2
	github.com/mst-corp/orbit/pkg v0.0.0
	github.com/mst-corp/orbit/services/messaging v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)
