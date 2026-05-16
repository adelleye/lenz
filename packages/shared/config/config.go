package config

type Config struct {
	DB
}

type Option func()

func New() *Config {
	db := NewDB()

	return &Config{DB: db}
}
