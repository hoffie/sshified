* v1.1.18
  - Metrics: Improve tracking of error types with new metric sshified_connection_errors_total
  - Fix timeout value in error message
  - Fix potential crash: avoid terminating ssh clients with active connections
  - Timeout keepalive earlier as we still need time budget for the reconnect
  - Update vendored dependencies

* v1.1.17
  - Enforce timeout and reconnect when awaiting an SSH keepalive reply to avoid long-stuck requests
  - Enforce timeout during inband port forwarding connect to ensure proper cleaning of stuck SSH connections
  - Add metric sshified_ssh_keepalive_failures_total for better visibility into broken SSH connections
* v1.1.16
  - Update vendored dependencies, especially golang.org/x/crypto for CVE-2023-48795
* v1.1.15
  - Make connecting to non-lowercased hostnames work by default
* v1.1.14
  - Downgrade build environment go go1.19 for rhel8 compatibility
* v1.1.13
  - Enable https support in cascaded sshified setups
* v1.1.12
  - Add validating https support via `__sshified_use_https=1`
  - Rename old `?__sshified_use_insecure_https=1` to `__sshified_https_insecure_skip_verify=1`
* v1.1.11
  - Add non-validating https support via `?__sshified_use_insecure_https=1` URL parameter
  - Update vendored dependencies
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
