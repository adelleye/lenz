package main

import (
	"lenz-core/apps/auth/authn"
	"lenz-core/apps/core/internal/corebanking"
	"lenz-core/apps/core/server"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("apps/core/.env")
	}

	s := server.NewServer(server.WithAuthn(authn.AuthRequiredScope, authn.AuthOptionalScope), server.WithRouter(routes))
	s.Run()
}

func routes(r *chi.Mux, deps server.Deps) {
	store := corebanking.NewSQLStore(deps.Cfg.DBConn)
	handler := corebanking.NewHandler(corebanking.NewService(store, corebanking.NewMockNIPProvider()))
	handler.Routes(r)
}
