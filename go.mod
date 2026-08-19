module github.com/dotcommander/distill

go 1.26.0

require (
	github.com/alecthomas/kong v1.16.1
	github.com/dotcommander/reliquary v0.12.0
	github.com/garyblankenship/wormhole/v3 v3.0.1
	github.com/pkoukk/tiktoken-go v0.1.8
	github.com/stretchr/testify v1.12.0
	golang.org/x/net v0.58.0
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.47.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/adrg/strutil v0.3.1 // indirect
	github.com/alecthomas/repr v0.5.4 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/yuin/goldmark v1.8.5 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/garyblankenship/wormhole/v3 => ../wormhole
