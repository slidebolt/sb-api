package app

import (
	"fmt"
	"os"
)

const DefaultPort = "29011"

// Config controls the importable API runtime.
type Config struct {
	ListenAddr string
	HTTPURL    string
}

func DefaultConfig() Config {
	httpURL := "127.0.0.1"
	if v := os.Getenv("SB_API_HTTP_URL"); v != "" {
		httpURL = v
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%s", DefaultPort)
	if v := os.Getenv("SB_API_PORT"); v != "" {
		listenAddr = fmt.Sprintf("127.0.0.1:%s", v)
	}
	if v := os.Getenv("SB_API_LISTEN_ADDR"); v != "" {
		listenAddr = v
	}

	return Config{
		ListenAddr: listenAddr,
		HTTPURL:    httpURL,
	}
}
