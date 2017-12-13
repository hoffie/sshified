# Testing
Load testing can be performed using ApacheBench (usually part of apache or apache-utils packages).
It is partly automated via a Makefile.
First, `go build` sshified, then `cd loadtest` and run the following steps.

## Preparations
```
make prepare
```

This will create a local user, generate an SSH keypair, set up the user for pubkey authentication and will populate a local known_hosts file.

## Setting up an example web server
```
make run-webserver
```
This will run an example webserver (go) on port 8080.

## Running sshified (second shell)
```
make run-sshified
```
This will run sshified with the previously generated key files and defined ports.

## Running the benchmark (third shell)
```
make run-loadtest
```

This will run ApacheBench (ab).

## Cleanup
```
make cleanup
```
This will reverse all actions done by `prepare`, i.e. will delete the user along with its home directory.
