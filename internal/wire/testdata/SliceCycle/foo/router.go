package main

type RouterService struct {
	handlers []Controller
}

func NewRouterService(handlers []Controller) *RouterService {
	return &RouterService{handlers: handlers}
}

func (r *RouterService) Len() int {
	return len(r.handlers)
}
