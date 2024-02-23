sshified: build

build:
	CGO_ENABLED=0 go build

check:
	errcheck
	golint
	go vet

.PHONY = build check
