module statesync

go 1.26.3

replace common_lib => ../common_lib

require (
	common_lib v0.0.0
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.19.0
	github.com/spf13/pflag v1.0.10
	go.uber.org/zap v1.28.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)
