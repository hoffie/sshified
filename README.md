# sshified
sshified acts as an HTTP proxy and forwards all received requests over server-specific SSH connections.

## Features
This tool is useful when you need to connect to several different machines using unauthenticated HTTP, but need benefits such as

  * authentication (via pubkey authentication and host key checking)
  * encryption (via SSH protocol)
  * limiting to a single connection/port (only SSH, port 22).

You will need to configure your software to use sshified as an HTTP proxy.

A popular use case is using the monitoring tool [Prometheus](https://prometheus.io).
By pointing Prometheus to sshified, all traffic will be tunneled over SSH and can therefore be run over untrusted networks.

### Non-features
Currently, no HTTPS (or general CONNECT) support is implemented or planned.
Rudimentary non-validating HTTPS client support exists by using the special `?__sshified_use_insecure_https=1` query parameter.

## Status
This project is considered feature-complete.
It has been used in production with several hundreds connections for many months now.

Synthetic tests can be run in order to prove stability.
For more details, see [TESTING](TESTING.md).

## Build
This tool is built using Go (tested with 1.9.2 or newer).
It makes use of some popular Go libraries, which have been vendored (using `dep`) to allow for reproducible builds and simplified cloning.

`go get -u github.com/hoffie/sshified`

## Configuration
### sshified
sshified is configured using command line options only (see `--help` and examples below).

### Target server configuration
All your target servers need to fullfil the following requirements:

* sshd server with the same port across your fleet
* a user (no shell access required; restricting the user via `ForceCommand` in `sshd_config` is recommended)
* public key authentication (`authorized_keys`)

The server running sshified is supposed to provide a `known_hosts` which contains entries for all possible targets.

It is recommended that this is managed using some configuration management tool such as Puppet.

## Run
```bash
$ ./sshified --proxy.listen-addr 127.0.0.1:8888 --ssh.user sshified-test --ssh.key-file conf/id_rsa --ssh.known-hosts-file conf/known_hosts -v
$ curl --proxy 127.0.0.1:8888 http://example.org:8080/api/example
```

In above example, the following will happen:

  * curl connects to the local sshified instance
  * sshified will establish a SSH connection to example.org
  * sshified will forward the HTTP request from curl to example.org using the previously created SSH connection.
  * the HTTP response will be returned in the opposite direction.
  
If another request is sent to example.org (which may even be to a different port), sshified will re-use the already existing SSH connection.
In other words: It uses a pooling strategy to minimize connection times and network traffic.
Should the connection fail, sshified will assume that the SSH tunnel may have been broken in the meantime (e.g. due to timeouts).
It will therefore retry connecting once.

## License
This software is released under the [Apache 2.0 license](LICENSE).

## Author
sshified has been created by [Christian Hoffmann](https://hoffmann-christian.info/).
If you find this project useful, please star it or drop me a short [mail](mailto:mail@hoffmann-christian.info).
