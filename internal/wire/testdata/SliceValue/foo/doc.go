package main

import (
	"fmt"
	"net/http"
)

type DocController struct{}

func (c DocController) InitRouter(mux *http.ServeMux) {
	mux.HandleFunc("/doc", c.doc)
}

func (c DocController) doc(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "doc")
}
