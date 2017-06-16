package main

import (
	"github.com/rook/rook/pkg/api"
)

func main() {

	errCh := make(chan error, 1)
	api.Start(errCh)
}
