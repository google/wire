package fuzz

import (
	"context"
	"github.com/google/wire/internal/wire"
	"os"
	"strings"
)

func Fuzz(data []byte) int {
	ctx := context.Background()
	_, err := wire.Load(ctx, "/", os.Environ(), strings.Split(string(data), " "))
	if err != nil {
		return 0
	}
	return 1
}
