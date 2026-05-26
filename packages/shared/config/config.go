package config

type Config struct {
	DB
}

type Option func()

func New() (*Config, error) {
	db, err := NewDB()
	if err != nil {
		return nil, err
	}

	return &Config{DB: db}, nil
}
