sshified: build

build:
	go build

check:
	errcheck
	golint
	go vet

.PHONY = build check
