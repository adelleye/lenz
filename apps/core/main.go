package main

import (
	"lenz-core/apps/auth/authn"
	"lenz-core/apps/core/internal/corebanking"
	"lenz-core/apps/core/server"
	"log"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("apps/core/.env")
	}

	s := server.NewServer(server.WithAuthn(authn.AuthRequiredScope), server.WithRouter(routes))
	s.Run()
}

func routes(r *chi.Mux, deps server.Deps) {
	repository := corebanking.NewSQLRepository(deps.Cfg.DBConn)
	demoRoutes, err := corebanking.DemoRoutesEnabled()
	if err != nil {
		log.Fatal(err)
	}
	var providers []corebanking.TransferProvider
	if demoRoutes {
		providers = append(providers, corebanking.NewMockNIPProvider())
	}
	handler := corebanking.NewHandler(
		corebanking.NewService(repository, providers...),
		corebanking.WithDemoRoutes(demoRoutes),
	)
	handler.Routes(r)
}
