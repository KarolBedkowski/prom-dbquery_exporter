package server

//
// security.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// tls, authorization configuration.
// based on github.com/prometheus/exporter-toolkit/web

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v2"
	"prom-dbquery_exporter.app/internal/metrics"
)

func serveHTTP(server *http.Server, listener net.Listener) error {
	log.Logger.Info().Msg("sechandler: TLS is disabled.")

	if err := server.Serve(listener); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// listenAndServe start webserver.
func listenAndServe(server *http.Server, tlsConfigPath string) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}

	defer listener.Close()

	if tlsConfigPath == "" {
		return serveHTTP(server, listener)
	}

	cfg, err := loadTLSConfig(tlsConfigPath)
	if err != nil {
		return fmt.Errorf("tls configuration error: %w", err)
	}

	var handler http.Handler = http.DefaultServeMux
	if server.Handler != nil {
		handler = server.Handler
	}

	server.Handler = newSecWebHandler(cfg, handler)

	config, err := web.ConfigToTLSConfig(&cfg.TLSConfig)
	if err == nil {
		if !cfg.HTTPConfig.HTTP2 {
			server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
		}
		// Valid TLS config.
		log.Logger.Info().Msg("sechandler: TLS is enabled.")
	} else {
		return serveHTTP(server, listener)
	}

	server.TLSConfig = config
	server.TLSConfig.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		config, err := getConfig(tlsConfigPath)
		if err != nil {
			return nil, err
		}

		tlsconf, err := web.ConfigToTLSConfig(&config.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("tls configuration error: %w", err)
		}

		tlsconf.NextProtos = server.TLSConfig.NextProtos

		return tlsconf, nil
	}

	if err := server.ServeTLS(listener, "", ""); err != nil {
		return fmt.Errorf("server tls error: %w", err)
	}

	return nil
}

func loadTLSConfig(tlsConfigPath string) (*web.Config, error) {
	if err := web.Validate(tlsConfigPath); err != nil {
		return nil, fmt.Errorf("validate tls config error: %w", err)
	}

	cfg, err := getConfig(tlsConfigPath)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func getConfig(configPath string) (*web.Config, error) {
	content, err := os.ReadFile(configPath) // #nosec
	if err != nil {
		return nil, fmt.Errorf("read config file error: %w", err)
	}

	cfg := &web.Config{
		TLSConfig: web.TLSConfig{ //nolint:exhaustruct
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
		},
		HTTPConfig: web.HTTPConfig{HTTP2: true, Header: nil},
		Users:      nil,
	}

	err = yaml.UnmarshalStrict(content, cfg)
	cfg.TLSConfig.SetDirectory(filepath.Dir(configPath))

	if err != nil {
		return cfg, fmt.Errorf("unmarshal config error: %w", err)
	}

	return cfg, nil
}

type secWebHandler struct {
	handler http.Handler
	cache   map[string]bool
	headers map[string]string
	users   map[string]config_util.Secret

	lock sync.Mutex
}

func newSecWebHandler(conf *web.Config, handler http.Handler) *secWebHandler {
	if cu := len(conf.Users); cu > 0 {
		log.Logger.Info().Int("users", cu).Msg("sechandler: authorization enabled")
	} else {
		log.Logger.Info().Msg("sechandler: authorization disabled")
	}

	return &secWebHandler{ //nolint:exhaustruct
		handler: handler,
		cache:   make(map[string]bool),
		headers: conf.HTTPConfig.Header,
		users:   conf.Users,
	}
}

func (wh *secWebHandler) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	// Configure http headers.
	for k, v := range wh.headers {
		writer.Header().Set(k, v)
	}

	if len(wh.users) == 0 {
		wh.handler.ServeHTTP(writer, req)

		return
	}

	user, pass, auth := req.BasicAuth()
	if auth {
		hashedPassword, validUser := wh.users[user]

		if !validUser {
			// The user is not found. Use a fixed password hash to
			// prevent user enumeration by timing requests.
			// This is a bcrypt-hashed version of "fakepassword".
			hashedPassword = "$2y$10$QOauhQNbBCuQDKes6eFzPeMqBSjb7Mr5DUmpZ/VcEd00UAV/LDeSi" // #nosec
		}

		cacheKey := hex.EncodeToString(append(append([]byte(user), []byte(hashedPassword)...),
			[]byte(pass)...))

		wh.lock.Lock()

		authOk, ok := wh.cache[cacheKey]
		if !ok {
			// This user, hashedPassword, password is not cached.
			err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(pass))
			authOk = err == nil
			wh.cache[cacheKey] = authOk
		}

		wh.lock.Unlock()

		if authOk && validUser {
			wh.handler.ServeHTTP(writer, req)

			return
		}
	}

	metrics.IncProcessErrorsCnt(metrics.ProcessAuthError)
	writer.Header().Set("WWW-Authenticate", "Basic")
	http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}
