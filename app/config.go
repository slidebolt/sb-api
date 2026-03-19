package app

// Config controls the importable API runtime.
type Config struct {
	ListenAddr string
	HTTPURL    string
}

func DefaultConfig() Config {
	return Config{
		ListenAddr: "127.0.0.1:29011",
		HTTPURL:    "127.0.0.1",
	}
}
