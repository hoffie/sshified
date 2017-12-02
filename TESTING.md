# Testing
Load testing can be performed using ApacheBench (usually part of apache or apache-utils packages).

## Preparations
```
$ mkdir conf/
```

## Setting up a test user
```
$ sudo useradd sshified-test -s /bin/false -m
$ ssh-keygen -f conf/id_rsa
$ sudo -u sshified-test mkdir /home/sshified-test/.ssh/
$ sudo -u sshified-test tee /home/sshified-test/.ssh/authorized_keys < conf/id_rsa.pub
$ sudo -u sshified-test chmod 700 /home/sshified-test/.ssh/
$ sudo -u sshified-test chmod 600 /home/sshified-test/.ssh/authorized_keys
```

## Populating known hosts file
```
$ ssh 127.0.0.1 -o UserKnownHostsFile=conf/known_hosts
# Add key, abort using Ctrl+C
```

## Setting up an example web server
```
$ cat > server.go <<EOF
package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/bar", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
EOF
$ go run server.go
```

## Running sshified (second shell)
```
$ ./sshified --proxy.listen-addr 127.0.0.1:8888 --ssh.user sshified-test --ssh.key-file conf/id_rsa --ssh.known-hosts-file conf/known_hosts
```

## Running the benchmark (third shell)
```
$ ab -n20000 -c500 -X 127.0.0.1:8888 http://127.0.0.1:8080/bar
```
