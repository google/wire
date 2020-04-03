package main

import (
	"fmt"
	"net/http"
)

type UploadController struct{}

func (c *UploadController) InitRouter(mux *http.ServeMux) {
	mux.HandleFunc("/upload", c.upload)
}

func (c *UploadController) upload(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "home")
}
