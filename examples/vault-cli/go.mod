module github.com/axsh/hag/examples/vault-cli

go 1.26.4

require (
	github.com/axsh/hag v0.0.0
	github.com/zalando/go-keyring v0.2.8
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/sys v0.45.0 // indirect
)

replace github.com/axsh/hag => ../../shared/libs/go
