package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Syntax: server.go PORT\n\n")
		fmt.Fprintf(os.Stderr, "Error: missing PORT\n")
		os.Exit(1)
	}
	http.HandleFunc("/bar", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", os.Args[1]), nil))
}
