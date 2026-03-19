package main

import (
	runtime "github.com/slidebolt/sb-runtime"

	"github.com/slidebolt/sb-api/app"
)

func main() {
	runtime.Run(app.New(app.DefaultConfig()))
}
