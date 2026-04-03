module github.com/mst-corp/orbit/services/gateway

go 1.24

replace github.com/mst-corp/orbit/pkg => ../../pkg

require (
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/alicebob/miniredis/v2 v2.37.0
	github.com/gofiber/contrib/websocket v1.3.2
	github.com/gofiber/fiber/v2 v2.52.6
	github.com/google/uuid v1.6.0
	github.com/mst-corp/orbit/pkg v0.0.0
	github.com/nats-io/nats.go v1.37.0
	github.com/redis/go-redis/v9 v9.7.0
	github.com/valyala/fasthttp v1.58.0
)

require (
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/fasthttp/websocket v1.5.8 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/savsgio/gotils v0.0.0-20240303185622-093b76447511 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/net v0.31.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
)
