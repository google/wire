package main

import (
	"fmt"
	"github.com/google/wire"
	"net/http"
)

var Set = wire.NewSet(NewHome, NewUpload)

type Controller interface {
	InitRouter(*http.ServeMux)
}

type Mux struct {
	Handlers []Controller
}

func InitRouter(handlers []Controller) *Mux {
	return &Mux{Handlers: handlers}
}

func main() {
	router := NewRouter()
	fmt.Println(len(router.Handlers))
}
