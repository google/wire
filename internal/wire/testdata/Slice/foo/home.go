package main

import (
	"fmt"
	"net/http"
)

type HomeController struct{}

func NewHome() *HomeController {
	return &HomeController{}
}

func (c *HomeController) InitRouter(mux *http.ServeMux) {
	mux.HandleFunc("/", c.home)
}

func (c *HomeController) home(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "home")
}
