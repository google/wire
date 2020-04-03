package main

import (
	"fmt"
	"net/http"
)

type HomeController struct {
	router *RouterService
}

func NewHome(router *RouterService) *HomeController {
	return &HomeController{
		router: router,
	}
}

func (c *HomeController) InitRouter(mux *http.ServeMux) {
	mux.HandleFunc("/", c.home)
}

func (c *HomeController) home(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "home: %d", c.router.Len())
}
