package main

import (
	"net/http"
)

var to string

func get_handler(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to, 301)
		return
	}
}

func http_redirect(from, to string) {
	http.Handle(from, get_handler(to))
}
