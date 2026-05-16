package config

import (
	"lenz-core/packages/shared/utils"
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

const (
	driverName = "postgres"
)

type DB struct {
	DBConn *sqlx.DB
}

func NewDB() DB {
	vp := viper.New()
	vp.AutomaticEnv()

	dataSourceName := vp.GetString(utils.EnvDatabaseURL)
	if dataSourceName == "" {
		dataSourceName = "postgres://lenzcore:lenzcore123@localhost:5432/lenzcore?sslmode=disable"
		log.Printf("DATABASE_URL not set, using local Docker Compose default")
	}

	db, err := sqlx.Connect(driverName, dataSourceName)
	if err != nil {
		panic(err)
	}

	// TODO: this values can be considered
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	return DB{DBConn: db}
}
