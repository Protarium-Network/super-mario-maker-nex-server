module github.com/Protarium-Network/super-mario-maker-nex-server

go 1.25.0

require (
	github.com/PretendoNetwork/nex-go/v2 v2.1.3
	github.com/PretendoNetwork/nex-protocols-common-go/v2 v2.4.0
	github.com/PretendoNetwork/nex-protocols-go/v2 v2.2.1
	github.com/PretendoNetwork/plogger-go v1.1.0
	github.com/joho/godotenv v1.5.1
	github.com/lib/pq v1.10.9
	github.com/minio/minio-go/v7 v7.0.90
)

require github.com/PretendoNetwork/ASH0 v0.0.0-20181015215447-8a175df3219a

require (
	github.com/dolthub/maphash v0.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jwalton/go-supportscolor v1.2.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/lxzan/gws v1.8.8 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/minio/crc64nvme v1.0.1 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/rasky/go-lzo v0.0.0-20200203143853-96a758eda86e // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/superwhiskers/crunch/v3 v3.5.7 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/term v0.43.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)

replace github.com/PretendoNetwork/nex-protocols-common-go/v2 => ./nex-protocols-common-go-patch
