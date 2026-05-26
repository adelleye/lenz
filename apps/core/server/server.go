package server

import (
	"context"
	"errors"
	"fmt"
	"lenz-core/apps/auth/authn"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lenz-core/packages/shared/config"
	"lenz-core/packages/shared/httpmiddleware"
	"lenz-core/packages/shared/utils"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"
	"github.com/spf13/viper"
)

const (
	writeTimeout              = 30 * time.Second
	readTimeout               = 5 * time.Second
	readHeaderTimeout         = 2 * time.Second
	idleTimeout               = 60 * time.Second
	timeoutMiddlewareDuration = 25 * time.Second
	maxHeaderBytes            = 1 << 20
)

type ServerOptions func(opts *Server) error

// Deps will be injected into our services
type Deps struct {
	Cfg   *config.Config
	Viper *viper.Viper
}

type Server struct {
	cfg        *config.Config
	viper      *viper.Viper
	cors       *cors.Cors
	router     *chi.Mux
	httpServer *http.Server
}

func NewServer(fns ...ServerOptions) (*Server, error) {
	s, err := defaultServerConf()
	if err != nil {
		return nil, err
	}
	s.initViper()

	s.setGlobalMiddlewares()

	for _, fn := range fns {
		if err := fn(s); err != nil {
			return nil, err
		}
	}

	return s, nil
}

func WithAuthn(scopes ...authn.AuthScope) ServerOptions {
	return func(opts *Server) error {
		if err := authn.ValidateDevelopmentAuthGuard(os.Getenv, scopes...); err != nil {
			return err
		}
		mw := authn.Authentication(scopes...)
		opts.router.Use(mw)

		return nil
	}
}

func WithRouter(fn func(r *chi.Mux, deps Deps)) ServerOptions {
	return func(opts *Server) error {
		if opts.router == nil {
			return fmt.Errorf("router is not configured")
		}

		var pinger dbPinger
		if opts.cfg != nil && opts.cfg.DBConn != nil {
			pinger = opts.cfg.DBConn
		}
		registerHealthRoutes(opts.router, pinger)

		fn(opts.router, Deps{
			Cfg:   opts.cfg,
			Viper: opts.viper,
		})

		return nil
	}
}

func (s *Server) initViper() {
	s.viper = viper.New()
	s.viper.AutomaticEnv()

	s.viper.SetDefault(utils.EnvPort, "3001")
}

func (s *Server) setGlobalMiddlewares() {
	s.router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message": "Endpoint does not exist"}`))
	})

	s.router.Use(middleware.RequestID)
	s.router.Use(httpmiddleware.Recover)
	s.router.Use(s.cors.Handler)
	s.router.Use(httpmiddleware.BodyLimit)
	s.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})

	s.router.Use(func(h http.Handler) http.Handler {
		return http.TimeoutHandler(h, timeoutMiddlewareDuration, `{"error":{"name":"timeout_error","message":"Request timed out"}}`)
	})

	s.router.Use(middleware.AllowContentType("application/json", "text/csv"))
}

func defaultServerConf() (*Server, error) {
	cfg, err := config.New()
	if err != nil {
		return nil, err
	}
	corsMiddleware, err := newCORSFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:    cfg,
		router: chi.NewRouter(),
		cors:   corsMiddleware,
	}, nil
}

func (s *Server) Run() {
	port := s.viper.GetString("PORT")
	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%s", port),
		Handler:           s.router,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	go func() {
		log.Printf("Server starting at %s\n", port)
		if err := s.httpServer.ListenAndServe(); shouldLogListenAndServeError(err) {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("Error shutting down server: %+v", err)
	}
}

func shouldLogListenAndServeError(err error) bool {
	return err != nil && !errors.Is(err, http.ErrServerClosed)
}
