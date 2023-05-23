* v1.1.10
  - Improve Host Key Algorithm choice by pre-scanning available known host key types. Previously, sshified was limited to using the first negotiated key type which could lead to failures if that type differs from the available known host key types.
  - Update vendored dependencies
* v1.1.9
  - Switch to go modules
  - Update vendored dependencies
  - First version with as official Github release with amd64 binary
* v1.1.8
  - Update vendored dependencies
* v1.1.7
  - Improve timeout handling with slow-read clients
* v1.1.6
  - Improvd timeout handling by the aligning different timeouts
* v1.1.5
  - Avoid unnecessary ssh reconnects when single ports fail to connect
  - Fix connect error when a single other port fails to connect
  - Update vendored dependencies
* v1.1.4
  - Avoid blocking during response header retrieval
  - Improve timeout handling
  - Update vendored dependencies
* v1.1.3
  - Improve inline-docs
  - Update vendored dependencies
* v1.1.2
  - Prometheus metric naming improvements
* v1.1.1
  - Prometheus metrics support
* v1.1
  - Support for cascading sshified (`--next-proxy.addr`)
* v1.0
  - Initial release
